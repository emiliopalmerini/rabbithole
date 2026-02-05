package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/epalmerini/rabbithole/internal/rabbitmq"
)

type browserView int

const (
	viewExchanges browserView = iota
	viewBindings
	viewCreateQueue
)

type browserModel struct {
	config        Config
	mgmt          *rabbitmq.ManagementClient
	width, height int

	view        browserView
	exchanges   []rabbitmq.Exchange
	queues      []rabbitmq.Queue
	bindings    []rabbitmq.Binding
	selectedIdx int
	scrollOff   int

	selectedExchange string
	routingKeyInput  textinput.Model
	queueNameInput   textinput.Model
	inputFocused     int  // 0 = queue name, 1 = routing key, 2 = durable toggle
	durableQueue     bool // create a persistent queue

	err     error
	loading bool
}

// Messages
type exchangesLoadedMsg struct {
	exchanges []rabbitmq.Exchange
	queues    []rabbitmq.Queue
}

type bindingsLoadedMsg struct {
	bindings []rabbitmq.Binding
}

type errorMsg struct {
	err error
}

type startConsumingMsg struct {
	exchange   string
	queue      string
	routingKey string
	durable    bool
}

func newBrowserModel(cfg Config) browserModel {
	queueInput := textinput.New()
	queueInput.Placeholder = "my-consumer-queue"
	queueInput.CharLimit = 100
	queueInput.Width = 40

	routingInput := textinput.New()
	routingInput.Placeholder = "#"
	routingInput.CharLimit = 200
	routingInput.Width = 40
	routingInput.SetValue("#")

	return browserModel{
		config:          cfg,
		view:            viewExchanges,
		routingKeyInput: routingInput,
		queueNameInput:  queueInput,
		loading:         true,
	}
}

func (m browserModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.loadTopology(),
	)
}

func (m browserModel) loadTopology() tea.Cmd {
	return func() tea.Msg {
		mgmt, err := rabbitmq.NewManagementClient(m.config.RabbitMQURL)
		if err != nil {
			return errorMsg{err: err}
		}

		exchanges, err := mgmt.GetExchanges("/")
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to load exchanges: %w", err)}
		}

		queues, err := mgmt.GetQueues("/")
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to load queues: %w", err)}
		}

		return exchangesLoadedMsg{exchanges: exchanges, queues: queues}
	}
}

func (m browserModel) loadBindings(exchange string) tea.Cmd {
	return func() tea.Msg {
		mgmt, err := rabbitmq.NewManagementClient(m.config.RabbitMQURL)
		if err != nil {
			return errorMsg{err: err}
		}

		bindings, err := mgmt.GetBindings("/", exchange)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to load bindings: %w", err)}
		}

		return bindingsLoadedMsg{bindings: bindings}
	}
}

func (m browserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle input mode
		if m.view == viewCreateQueue {
			switch msg.String() {
			case "esc":
				m.view = viewBindings
				m.queueNameInput.Blur()
				m.routingKeyInput.Blur()
				return m, nil
			case "tab", "shift+tab":
				// Cycle through: queue name (0) -> routing key (1) -> durable (2)
				m.queueNameInput.Blur()
				m.routingKeyInput.Blur()
				if msg.String() == "tab" {
					m.inputFocused = (m.inputFocused + 1) % 3
				} else {
					m.inputFocused = (m.inputFocused + 2) % 3
				}
				if m.inputFocused == 0 {
					m.queueNameInput.Focus()
				} else if m.inputFocused == 1 {
					m.routingKeyInput.Focus()
				}
				return m, nil
			case " ":
				// Toggle durable when focused on it
				if m.inputFocused == 2 {
					m.durableQueue = !m.durableQueue
					return m, nil
				}
			case "enter":
				queueName := m.queueNameInput.Value()
				routingKey := m.routingKeyInput.Value()
				if queueName == "" {
					queueName = fmt.Sprintf("rabbithole-%s", randomSuffix())
				}
				if routingKey == "" {
					routingKey = "#"
				}
				durable := m.durableQueue
				return m, func() tea.Msg {
					return startConsumingMsg{
						exchange:   m.selectedExchange,
						queue:      queueName,
						routingKey: routingKey,
						durable:    durable,
					}
				}
			}
			// Handle text input for queue name and routing key fields
			if m.inputFocused == 0 || m.inputFocused == 1 {
				var cmd tea.Cmd
				if m.inputFocused == 0 {
					m.queueNameInput, cmd = m.queueNameInput.Update(msg)
				} else {
					m.routingKeyInput, cmd = m.routingKeyInput.Update(msg)
				}
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
				if m.selectedIdx < m.scrollOff {
					m.scrollOff = m.selectedIdx
				}
			}
		case "down", "j":
			maxIdx := m.maxIndex()
			if m.selectedIdx < maxIdx {
				m.selectedIdx++
				visibleItems := m.height - 10
				if m.selectedIdx >= m.scrollOff+visibleItems {
					m.scrollOff++
				}
			}
		case "enter":
			switch m.view {
			case viewExchanges:
				if m.selectedIdx < len(m.exchanges) {
					m.selectedExchange = m.exchanges[m.selectedIdx].Name
					m.view = viewBindings
					m.selectedIdx = 0
					m.scrollOff = 0
					m.loading = true
					return m, m.loadBindings(m.selectedExchange)
				}
			case viewBindings:
				// "New binding" option is at index 0
				if m.selectedIdx == 0 {
					m.view = viewCreateQueue
					m.queueNameInput.Focus()
					m.inputFocused = 0
					return m, textinput.Blink
				}
				// Existing bindings
				if m.selectedIdx-1 < len(m.bindings) {
					binding := m.bindings[m.selectedIdx-1]
					return m, func() tea.Msg {
						return startConsumingMsg{
							exchange:   m.selectedExchange,
							queue:      binding.Destination,
							routingKey: binding.RoutingKey,
						}
					}
				}
			}
		case "esc", "backspace":
			if m.view == viewBindings {
				m.view = viewExchanges
				m.selectedIdx = 0
				m.scrollOff = 0
				m.selectedExchange = ""
			}
		case "r":
			m.loading = true
			return m, m.loadTopology()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case exchangesLoadedMsg:
		m.loading = false
		m.exchanges = msg.exchanges
		m.queues = msg.queues
		mgmt, _ := rabbitmq.NewManagementClient(m.config.RabbitMQURL)
		m.mgmt = mgmt

	case bindingsLoadedMsg:
		m.loading = false
		m.bindings = msg.bindings

	case errorMsg:
		m.loading = false
		m.err = msg.err

	case startConsumingMsg:
		// This will be handled by the parent to switch to consumer view
		return m, nil
	}

	return m, tea.Batch(cmds...)
}

func (m browserModel) maxIndex() int {
	switch m.view {
	case viewExchanges:
		return len(m.exchanges) - 1
	case viewBindings:
		return len(m.bindings) // +1 for "new binding" option, -1 for 0-index
	default:
		return 0
	}
}

func (m browserModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	header := headerStyle.Width(m.width - 2).Render(
		"🐰 rabbithole - Topology Browser",
	)

	var content string
	switch m.view {
	case viewExchanges:
		content = m.renderExchanges()
	case viewBindings:
		content = m.renderBindings()
	case viewCreateQueue:
		content = m.renderCreateQueue()
	}

	help := m.renderHelp()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		content,
		help,
	)
}

func (m browserModel) renderExchanges() string {
	var sb strings.Builder

	sb.WriteString(fieldNameStyle.Render("Select an Exchange:"))
	sb.WriteString("\n\n")

	if m.loading {
		sb.WriteString(mutedStyle.Render("  Loading..."))
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	if m.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	if len(m.exchanges) == 0 {
		sb.WriteString(mutedStyle.Render("  No exchanges found"))
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	visibleItems := m.height - 12
	if visibleItems < 1 {
		visibleItems = 1
	}
	endIdx := m.scrollOff + visibleItems
	if endIdx > len(m.exchanges) {
		endIdx = len(m.exchanges)
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}

	for i := m.scrollOff; i < endIdx; i++ {
		ex := m.exchanges[i]
		typeStr := mutedStyle.Render(fmt.Sprintf("[%s]", ex.Type))
		durableStr := ""
		if ex.Durable {
			durableStr = mutedStyle.Render(" durable")
		}

		line := fmt.Sprintf("%s %s%s", ex.Name, typeStr, durableStr)

		if i == m.selectedIdx {
			sb.WriteString(selectedMessageStyle.Width(m.width - 8).Render("▶ " + line))
		} else {
			sb.WriteString(normalMessageStyle.Width(m.width - 8).Render("  " + line))
		}
		sb.WriteString("\n")
	}

	return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
}

func (m browserModel) renderBindings() string {
	var sb strings.Builder

	sb.WriteString(fieldNameStyle.Render(fmt.Sprintf("Exchange: %s", m.selectedExchange)))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("Select a binding or create new:"))
	sb.WriteString("\n\n")

	if m.loading {
		sb.WriteString(mutedStyle.Render("  Loading..."))
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	// First option: create new binding
	newBindingLine := "➕ Create new queue & binding..."
	if m.selectedIdx == 0 {
		sb.WriteString(selectedMessageStyle.Width(m.width - 8).Render("▶ " + newBindingLine))
	} else {
		sb.WriteString(normalMessageStyle.Width(m.width - 8).Render("  " + newBindingLine))
	}
	sb.WriteString("\n\n")

	if len(m.bindings) == 0 {
		sb.WriteString(mutedStyle.Render("  No existing bindings"))
	} else {
		sb.WriteString(mutedStyle.Render("  Existing bindings:"))
		sb.WriteString("\n")

		visibleItems := m.height - 16
		if visibleItems < 1 {
			visibleItems = 1
		}
		startIdx := 0
		if m.scrollOff > 0 {
			startIdx = m.scrollOff - 1 // Account for "new binding" option
		}
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := startIdx + visibleItems
		if endIdx > len(m.bindings) {
			endIdx = len(m.bindings)
		}

		for i := startIdx; i < endIdx; i++ {
			b := m.bindings[i]
			routingKey := b.RoutingKey
			if routingKey == "" {
				routingKey = "(empty)"
			}
			line := fmt.Sprintf("%s → %s", routingKeyStyle.Render(routingKey), b.Destination)

			actualIdx := i + 1 // +1 because of "new binding" at index 0
			if actualIdx == m.selectedIdx {
				sb.WriteString(selectedMessageStyle.Width(m.width - 8).Render("  ▶ " + line))
			} else {
				sb.WriteString(normalMessageStyle.Width(m.width - 8).Render("    " + line))
			}
			sb.WriteString("\n")
		}
	}

	return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
}

func (m browserModel) renderCreateQueue() string {
	var sb strings.Builder

	sb.WriteString(fieldNameStyle.Render(fmt.Sprintf("Create binding on: %s", m.selectedExchange)))
	sb.WriteString("\n\n")

	// Queue name input
	queueLabel := "Queue name: "
	if m.inputFocused == 0 {
		queueLabel = selectedMessageStyle.Render("▶ Queue name: ")
	} else {
		queueLabel = normalMessageStyle.Render("  Queue name: ")
	}
	sb.WriteString(queueLabel)
	sb.WriteString(m.queueNameInput.View())
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("    (leave empty for auto-generated name)"))
	sb.WriteString("\n\n")

	// Routing key input
	routingLabel := "Routing key: "
	if m.inputFocused == 1 {
		routingLabel = selectedMessageStyle.Render("▶ Routing key: ")
	} else {
		routingLabel = normalMessageStyle.Render("  Routing key: ")
	}
	sb.WriteString(routingLabel)
	sb.WriteString(m.routingKeyInput.View())
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("    (use # for all, * for single word wildcard)"))
	sb.WriteString("\n\n")

	// Durable toggle
	durableLabel := "Durable: "
	checkbox := "[ ]"
	if m.durableQueue {
		checkbox = "[✓]"
	}
	if m.inputFocused == 2 {
		durableLabel = selectedMessageStyle.Render("▶ Durable: ")
		checkbox = selectedMessageStyle.Render(checkbox)
	} else {
		durableLabel = normalMessageStyle.Render("  Durable: ")
	}
	sb.WriteString(durableLabel)
	sb.WriteString(checkbox)
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("    (persistent queue survives restarts, press Space to toggle)"))
	sb.WriteString("\n\n")

	sb.WriteString(helpStyle.Render("Press Enter to create and start consuming, Esc to cancel"))

	return detailPanelStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
}

func (m browserModel) renderHelp() string {
	var keys []struct{ key, desc string }

	switch m.view {
	case viewExchanges:
		keys = []struct{ key, desc string }{
			{"↑/k", "up"},
			{"↓/j", "down"},
			{"enter", "select"},
			{"r", "refresh"},
			{"q", "quit"},
		}
	case viewBindings:
		keys = []struct{ key, desc string }{
			{"↑/k", "up"},
			{"↓/j", "down"},
			{"enter", "select"},
			{"esc", "back"},
			{"q", "quit"},
		}
	case viewCreateQueue:
		keys = []struct{ key, desc string }{
			{"tab", "next field"},
			{"enter", "create"},
			{"esc", "cancel"},
		}
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %s", helpKeyStyle.Render(k.key), k.desc))
	}

	return helpStyle.Render(strings.Join(parts, "  │  "))
}

func randomSuffix() string {
	// Simple random suffix - reuse from consumer.go
	return fmt.Sprintf("%d", time.Now().UnixNano()%100000)
}
