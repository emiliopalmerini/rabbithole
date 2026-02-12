package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/db"
)

type appView int

const (
	appViewBrowser appView = iota
	appViewConsumer
)

type appModel struct {
	config Config
	store  db.Store
	view   appView

	browser  browserModel
	consumer model

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

func (m appModel) Init() tea.Cmd {
	return m.browser.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

	case tea.KeyMsg:
		// Global escape to go back to browser from consumer
		if m.view == appViewConsumer && msg.String() == "b" && !m.consumer.searchMode {
			// Clean up consumer resources on navigate-away
			m.consumer.cleanup()

			m.view = appViewBrowser
			// Sync created queues back to browser
			for q := range m.createdQueues {
				m.browser.createdQueues[q] = true
			}
			return m, m.browser.loadTopology()
		}
	}

	switch m.view {
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
	}

	return m, nil
}

func (m appModel) View() string {
	switch m.view {
	case appViewBrowser:
		return m.browser.View()
	case appViewConsumer:
		return m.consumer.View()
	}
	return ""
}
