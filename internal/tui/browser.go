package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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

	// Search/filter
	searchMode   bool
	searchInput  textinput.Model
	filterQuery  string
	filteredList []int // indices of filtered items

	// Created queues tracking (for deletion)
	createdQueues map[string]bool

	// Spinner
	spinner spinner.Model

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

type queueDeletedMsg struct {
	queue string
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

	searchInput := textinput.New()
	searchInput.Placeholder = "Filter..."
	searchInput.CharLimit = 50
	searchInput.Width = 30

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	return browserModel{
		config:          cfg,
		view:            viewExchanges,
		routingKeyInput: routingInput,
		queueNameInput:  queueInput,
		searchInput:     searchInput,
		createdQueues:   make(map[string]bool),
		spinner:         sp,
		loading:         true,
	}
}

func (m browserModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.loadTopology(),
		m.spinner.Tick,
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

func (m browserModel) deleteQueue(queueName string) tea.Cmd {
	return func() tea.Msg {
		mgmt, err := rabbitmq.NewManagementClient(m.config.RabbitMQURL)
		if err != nil {
			return errorMsg{err: err}
		}

		err = mgmt.DeleteQueue("/", queueName)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to delete queue: %w", err)}
		}

		return queueDeletedMsg{queue: queueName}
	}
}

func (m browserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle search mode input
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.filterQuery = ""
				m.filteredList = nil
				m.searchInput.SetValue("")
				m.searchInput.Blur()
				return m, nil
			case "enter":
				m.searchMode = false
				m.filterQuery = m.searchInput.Value()
				m.searchInput.Blur()
				m.applyFilter()
				m.selectedIdx = 0
				m.scrollOff = 0
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				// Live filtering as user types
				m.filterQuery = m.searchInput.Value()
				m.applyFilter()
				return m, cmd
			}
		}

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
				switch m.inputFocused {
				case 0:
					m.queueNameInput.Focus()
				case 1:
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
				// Track created queue
				m.createdQueues[queueName] = true
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
		case "/":
			m.searchMode = true
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink
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
		case "g":
			m.selectedIdx = 0
			m.scrollOff = 0
		case "G":
			maxIdx := m.maxIndex()
			if maxIdx >= 0 {
				m.selectedIdx = maxIdx
			}
		case "d":
			// Delete queue (only if created this session)
			if m.view == viewBindings && m.selectedIdx > 0 {
				bindingIdx := m.selectedIdx - 1
				if bindingIdx < len(m.bindings) {
					queueName := m.bindings[bindingIdx].Destination
					if m.createdQueues[queueName] {
						delete(m.createdQueues, queueName)
						return m, m.deleteQueue(queueName)
					}
				}
			}
		case "enter":
			switch m.view {
			case viewExchanges:
				idx := m.getActualIndex(m.selectedIdx)
				if idx >= 0 && idx < len(m.exchanges) {
					m.selectedExchange = m.exchanges[idx].Name
					m.view = viewBindings
					m.selectedIdx = 0
					m.scrollOff = 0
					m.loading = true
					m.filterQuery = ""
					m.filteredList = nil
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
				m.filterQuery = ""
				m.filteredList = nil
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
		m.applyFilter()

	case bindingsLoadedMsg:
		m.loading = false
		m.bindings = msg.bindings

	case queueDeletedMsg:
		// Reload bindings after deletion
		m.loading = true
		return m, m.loadBindings(m.selectedExchange)

	case errorMsg:
		m.loading = false
		m.err = msg.err

	case startConsumingMsg:
		// This will be handled by the parent to switch to consumer view
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *browserModel) applyFilter() {
	if m.filterQuery == "" || m.view != viewExchanges {
		m.filteredList = nil
		return
	}

	query := strings.ToLower(m.filterQuery)
	m.filteredList = nil
	for i, ex := range m.exchanges {
		if strings.Contains(strings.ToLower(ex.Name), query) {
			m.filteredList = append(m.filteredList, i)
		}
	}
}

func (m browserModel) getActualIndex(displayIdx int) int {
	if m.filteredList == nil {
		return displayIdx
	}
	if displayIdx >= 0 && displayIdx < len(m.filteredList) {
		return m.filteredList[displayIdx]
	}
	return -1
}

func (m browserModel) maxIndex() int {
	switch m.view {
	case viewExchanges:
		if m.filteredList != nil {
			return len(m.filteredList) - 1
		}
		return len(m.exchanges) - 1
	case viewBindings:
		return len(m.bindings) // +1 for "new binding" option, -1 for 0-index
	default:
		return 0
	}
}

func (m browserModel) View() string {
	if m.width == 0 {
		return m.spinner.View() + " Loading..."
	}

	header := headerStyle.Width(m.width - 2).Render(
		"rabbithole - Topology Browser",
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

	var bottomBar string
	if m.searchMode {
		bottomBar = helpStyle.Render("Filter: ") + m.searchInput.View() + helpStyle.Render("  (Enter to apply, Esc to cancel)")
	} else {
		bottomBar = m.renderHelp()
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		content,
		bottomBar,
	)
}

func (m browserModel) renderExchanges() string {
	var sb strings.Builder

	title := "Select an Exchange:"
	if m.filterQuery != "" {
		title = fmt.Sprintf("Select an Exchange (filtered: %q):", m.filterQuery)
	}
	sb.WriteString(fieldNameStyle.Render(title))
	sb.WriteString("\n\n")

	if m.loading {
		sb.WriteString("  " + m.spinner.View() + " Loading...")
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	if m.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	displayList := m.exchanges
	if m.filteredList != nil {
		displayList = make([]rabbitmq.Exchange, len(m.filteredList))
		for i, idx := range m.filteredList {
			displayList[i] = m.exchanges[idx]
		}
	}

	if len(displayList) == 0 {
		if m.filterQuery != "" {
			sb.WriteString(mutedStyle.Render("  No exchanges match filter"))
		} else {
			sb.WriteString(mutedStyle.Render("  No exchanges found"))
		}
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	visibleItems := m.height - 12
	if visibleItems < 1 {
		visibleItems = 1
	}
	endIdx := m.scrollOff + visibleItems
	if endIdx > len(displayList) {
		endIdx = len(displayList)
	}
	startIdx := m.scrollOff
	if startIdx < 0 {
		startIdx = 0
	}

	for i := startIdx; i < endIdx; i++ {
		ex := displayList[i]
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
		sb.WriteString("  " + m.spinner.View() + " Loading...")
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	// First option: create new binding
	newBindingLine := "+ Create new queue & binding..."
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

			// Show indicator if queue was created this session (can be deleted)
			deleteHint := ""
			if m.createdQueues[b.Destination] {
				deleteHint = mutedStyle.Render(" [d to delete]")
			}

			line := fmt.Sprintf("%s → %s%s", routingKeyStyle.Render(routingKey), b.Destination, deleteHint)

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
		checkbox = "[x]"
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
			{"j/k", "navigate"},
			{"/", "filter"},
			{"enter", "select"},
			{"r", "refresh"},
			{"q", "quit"},
		}
	case viewBindings:
		keys = []struct{ key, desc string }{
			{"j/k", "navigate"},
			{"enter", "select"},
			{"d", "delete"},
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
