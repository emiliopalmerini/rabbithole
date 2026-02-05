package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/proto"
)

type appView int

const (
	appViewBrowser appView = iota
	appViewConsumer
)

type appModel struct {
	config   Config
	view     appView
	browser  browserModel
	consumer model
	decoder  *proto.Decoder
}

func newAppModel(cfg Config) appModel {
	return appModel{
		config:  cfg,
		view:    appViewBrowser,
		browser: newBrowserModel(cfg),
	}
}

func (m appModel) Init() tea.Cmd {
	return m.browser.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case startConsumingMsg:
		// Switch to consumer view
		m.view = appViewConsumer
		consumerCfg := m.config
		consumerCfg.Exchange = msg.exchange
		consumerCfg.QueueName = msg.queue
		consumerCfg.RoutingKey = msg.routingKey
		consumerCfg.Durable = msg.durable
		m.consumer = initialModel(consumerCfg)
		m.consumer.width = m.browser.width
		m.consumer.height = m.browser.height
		return m, m.consumer.Init()

	case tea.KeyMsg:
		// Global escape to go back to browser from consumer
		if m.view == appViewConsumer && msg.String() == "b" {
			m.view = appViewBrowser
			return m, m.browser.loadTopology()
		}
	}

	switch m.view {
	case appViewBrowser:
		newBrowser, cmd := m.browser.Update(msg)
		m.browser = newBrowser.(browserModel)
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
