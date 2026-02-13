package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/config"
	"github.com/epalmerini/rabbithole/internal/db"
	"github.com/epalmerini/rabbithole/internal/proto"
)

type appView int

const (
	appViewBrowser appView = iota
	appViewConsumer
	appViewSessionBrowser
	appViewProfilePicker
	appViewURLPrompt
)

type appModel struct {
	config    Config
	fileCfg   *config.FileConfig
	configDir string
	store     db.Store
	view      appView

	browser        browserModel
	consumer       model
	sessionBrowser sessionBrowserModel
	profilePicker  profilePickerModel
	urlPrompt      urlPromptModel

	// Track queues created across views for deletion
	createdQueues map[string]bool
}

func newAppModel(cfg Config, store db.Store) appModel {
	return appModel{
		config:        cfg,
		store:         store,
		view:          appViewBrowser,
		browser:       newBrowserModel(cfg),
		createdQueues: make(map[string]bool),
	}
}

func newAppModelWithProfilePicker(fileCfg *config.FileConfig, configDir string, store db.Store) appModel {
	return appModel{
		fileCfg:       fileCfg,
		configDir:     configDir,
		store:         store,
		view:          appViewProfilePicker,
		profilePicker: newProfilePickerModel(fileCfg.Profiles),
		createdQueues: make(map[string]bool),
	}
}

func newAppModelWithURLPrompt(fileCfg *config.FileConfig, configDir string, store db.Store) appModel {
	return appModel{
		fileCfg:       fileCfg,
		configDir:     configDir,
		store:         store,
		view:          appViewURLPrompt,
		urlPrompt:     newURLPromptModel(),
		createdQueues: make(map[string]bool),
	}
}

// resolveAndInitBrowser resolves the config for a given profile/URL and creates the browser.
func (m *appModel) resolveAndInitBrowser(profileName string, url string) tea.Cmd {
	var cfg Config

	if m.fileCfg != nil {
		resolved := m.fileCfg.Resolve(profileName, m.configDir)
		cfg = Config{
			RabbitMQURL:       resolved.RabbitMQURL,
			ManagementURL:     resolved.ManagementURL,
			ProtoPath:         resolved.ProtoPath,
			DBPath:            resolved.DBPath,
			MaxMessages:       resolved.MaxMessages,
			DefaultSplitRatio: resolved.DefaultSplitRatio,
			CompactMode:       resolved.CompactMode,
			ConfigDir:         resolved.ConfigDir,
		}
	}

	// Override URL if provided directly (from URL prompt)
	if url != "" {
		cfg.RabbitMQURL = url
	}

	// Initialize proto decoder if path provided
	if cfg.ProtoPath != "" {
		dec, err := proto.NewDecoder(cfg.ProtoPath)
		if err == nil {
			cfg.Decoder = dec
		}
	}

	m.config = cfg
	m.view = appViewBrowser
	m.browser = newBrowserModel(cfg)
	return m.browser.Init()
}

func (m appModel) Init() tea.Cmd {
	switch m.view {
	case appViewProfilePicker:
		return m.profilePicker.Init()
	case appViewURLPrompt:
		return tea.Batch(m.urlPrompt.Init(), tea.EnterAltScreen)
	case appViewBrowser:
		return m.browser.Init()
	}
	return nil
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case profileSelectedMsg:
		return m, m.resolveAndInitBrowser(msg.name, "")

	case urlEnteredMsg:
		return m, m.resolveAndInitBrowser("", msg.url)

	case startConsumingMsg:
		// Track newly created queue
		if msg.queue != "" {
			m.createdQueues[msg.queue] = true
		}
		// Copy created queues to browser for tracking
		for q := range m.createdQueues {
			m.browser.createdQueues[q] = true
		}

		// Clean up previous consumer before starting a new one
		m.consumer.cleanup()

		// Switch to consumer view
		m.view = appViewConsumer
		consumerCfg := m.config
		consumerCfg.Exchange = msg.exchange
		consumerCfg.QueueName = msg.queue
		consumerCfg.RoutingKey = msg.routingKey
		consumerCfg.Durable = msg.durable
		m.consumer = initialModel(consumerCfg, m.store)
		m.consumer.width = m.browser.width
		m.consumer.height = m.browser.height
		return m, m.consumer.Init()

	case replaySessionMsg:
		// Clean up previous consumer before replaying
		m.consumer.cleanup()

		m.view = appViewConsumer
		replayCfg := m.config
		replayCfg.Exchange = msg.session.Exchange
		replayCfg.RoutingKey = msg.session.RoutingKey
		m.consumer = initialReplayModel(replayCfg, msg.session, msg.messages)
		m.consumer.width = m.sessionBrowser.width
		m.consumer.height = m.sessionBrowser.height
		return m, nil

	case tea.KeyMsg:
		// Global escape to go back to browser from consumer
		if m.view == appViewConsumer && msg.String() == "b" && !m.consumer.searchMode && !m.consumer.filterMode {
			// Clean up consumer resources on navigate-away
			m.consumer.cleanup()

			// If we came from session browser (replay mode), go back there
			if m.consumer.replayMode {
				m.view = appViewSessionBrowser
				return m, m.sessionBrowser.loadSessions()
			}

			m.view = appViewBrowser
			// Sync created queues back to browser
			for q := range m.createdQueues {
				m.browser.createdQueues[q] = true
			}
			return m, m.browser.loadTopology()
		}

		// Switch to session browser from topology browser
		if m.view == appViewBrowser && msg.String() == "s" && !m.browser.searchMode && m.store != nil {
			m.view = appViewSessionBrowser
			m.sessionBrowser = newSessionBrowserModel(m.config, m.store)
			m.sessionBrowser.width = m.browser.width
			m.sessionBrowser.height = m.browser.height
			return m, m.sessionBrowser.Init()
		}

		// Back to topology browser from session browser
		if m.view == appViewSessionBrowser && msg.String() == "b" && !m.sessionBrowser.searchMode && !m.sessionBrowser.ftsMode && !m.sessionBrowser.confirmDelete {
			m.view = appViewBrowser
			return m, m.browser.loadTopology()
		}
	}

	switch m.view {
	case appViewProfilePicker:
		newPicker, cmd := m.profilePicker.Update(msg)
		m.profilePicker = newPicker.(profilePickerModel)
		return m, cmd

	case appViewURLPrompt:
		newPrompt, cmd := m.urlPrompt.Update(msg)
		m.urlPrompt = newPrompt.(urlPromptModel)
		return m, cmd

	case appViewBrowser:
		newBrowser, cmd := m.browser.Update(msg)
		m.browser = newBrowser.(browserModel)
		// Sync created queues from browser
		for q := range m.browser.createdQueues {
			m.createdQueues[q] = true
		}
		return m, cmd

	case appViewConsumer:
		newConsumer, cmd := m.consumer.Update(msg)
		m.consumer = newConsumer.(model)
		return m, cmd

	case appViewSessionBrowser:
		newSB, cmd := m.sessionBrowser.Update(msg)
		m.sessionBrowser = newSB.(sessionBrowserModel)
		return m, cmd
	}

	return m, nil
}

func (m appModel) View() string {
	switch m.view {
	case appViewProfilePicker:
		return m.profilePicker.View()
	case appViewURLPrompt:
		return m.urlPrompt.View()
	case appViewBrowser:
		return m.browser.View()
	case appViewConsumer:
		return m.consumer.View()
	case appViewSessionBrowser:
		return m.sessionBrowser.View()
	}
	return ""
}
