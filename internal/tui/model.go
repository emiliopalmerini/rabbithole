package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type connectionState int

const (
	stateDisconnected connectionState = iota
	stateConnecting
	stateConnected
)

type model struct {
	config        Config
	messages      []Message
	selectedIdx   int
	messageCount  int
	connState     connectionState
	connError     error
	width, height int
	viewport      viewport.Model
	showRaw       bool
	paused        bool
	msgChan       <-chan Message
}

// Tea messages
type msgReceived struct {
	msg Message
}

type connectedMsg struct {
	msgChan <-chan Message
}

type connectionErrorMsg struct {
	err error
}

type tickMsg time.Time

func initialModel(cfg Config) model {
	return model{
		config:    cfg,
		messages:  make([]Message, 0, 1000),
		connState: stateDisconnected,
		viewport:  viewport.New(80, 20),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.connectCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "down", "j":
			if m.selectedIdx < len(m.messages)-1 {
				m.selectedIdx++
			}
		case "g":
			m.selectedIdx = 0
		case "G":
			if len(m.messages) > 0 {
				m.selectedIdx = len(m.messages) - 1
			}
		case "r":
			m.showRaw = !m.showRaw
		case "p", " ":
			m.paused = !m.paused
		case "c":
			m.messages = m.messages[:0]
			m.selectedIdx = 0
			m.messageCount = 0
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width/2 - 4
		m.viewport.Height = msg.Height - 8

	case connectedMsg:
		m.connState = stateConnected
		m.msgChan = msg.msgChan
		cmds = append(cmds, m.waitForMessage())

	case connectionErrorMsg:
		m.connState = stateDisconnected
		m.connError = msg.err

	case msgReceived:
		if !m.paused {
			m.messageCount++
			msg.msg.ID = m.messageCount
			m.messages = append(m.messages, msg.msg)
			// Keep max 1000 messages
			if len(m.messages) > 1000 {
				m.messages = m.messages[1:]
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
			}
			// Auto-scroll to latest if at bottom
			if m.selectedIdx == len(m.messages)-2 {
				m.selectedIdx = len(m.messages) - 1
			}
		}
		cmds = append(cmds, m.waitForMessage())
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Header
	header := headerStyle.Width(m.width - 2).Render(
		"🐰 rabbithole - RabbitMQ Message Inspector",
	)

	// Status bar
	status := m.renderStatusBar()

	// Main content: message list + detail panel
	listWidth := m.width/2 - 2
	detailWidth := m.width - listWidth - 4

	messageList := m.renderMessageList(listWidth, m.height-8)
	detailPanel := m.renderDetailPanel(detailWidth, m.height-8)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		messageList,
		detailPanel,
	)

	// Help bar
	help := m.renderHelpBar()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		status,
		content,
		help,
	)
}

func (m model) renderStatusBar() string {
	var connStatus string
	switch m.connState {
	case stateConnected:
		connStatus = connectedStyle.Render("● Connected")
	case stateConnecting:
		connStatus = statusBarStyle.Render("◌ Connecting...")
	default:
		errMsg := ""
		if m.connError != nil {
			errMsg = fmt.Sprintf(" (%s)", m.connError.Error())
		}
		connStatus = disconnectedStyle.Render("○ Disconnected" + errMsg)
	}

	exchange := statusBarStyle.Render(fmt.Sprintf("Exchange: %s", m.config.Exchange))
	routingKey := statusBarStyle.Render(fmt.Sprintf("Routing: %s", m.config.RoutingKey))
	msgCount := statusBarStyle.Render(fmt.Sprintf("Messages: %d", len(m.messages)))
	protoCount := statusBarStyle.Render(fmt.Sprintf("Proto: %d types", protoTypesLoaded))

	pausedStatus := ""
	if m.paused {
		pausedStatus = disconnectedStyle.Render(" [PAUSED]")
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		connStatus,
		pausedStatus,
		"  │  ",
		exchange,
		"  │  ",
		routingKey,
		"  │  ",
		msgCount,
		"  │  ",
		protoCount,
	)
}

func (m model) renderMessageList(width, height int) string {
	visibleItems := height - 2
	if visibleItems < 1 {
		visibleItems = 1
	}

	startIdx := 0
	if m.selectedIdx >= visibleItems {
		startIdx = m.selectedIdx - visibleItems + 1
	}

	endIdx := startIdx + visibleItems
	if endIdx > len(m.messages) {
		endIdx = len(m.messages)
	}

	// Pre-allocate with exact capacity for consistent rendering
	items := make([]string, 0, visibleItems)

	for i := startIdx; i < endIdx; i++ {
		msg := m.messages[i]
		ts := timestampStyle.Render(msg.Timestamp.Format("15:04:05"))
		rk := routingKeyStyle.Render(truncate(msg.RoutingKey, width-20))
		line := fmt.Sprintf("%s %s", ts, rk)

		if i == m.selectedIdx {
			line = selectedMessageStyle.Width(width - 4).Render("▶ " + line)
		} else {
			line = normalMessageStyle.Width(width - 4).Render("  " + line)
		}
		items = append(items, line)
	}

	// Pad with empty lines to maintain fixed height
	emptyLine := normalMessageStyle.Width(width - 4).Render("")
	for len(items) < visibleItems {
		items = append(items, emptyLine)
	}

	content := strings.Join(items, "\n")
	if len(m.messages) == 0 {
		content = mutedStyle.Render("  Waiting for messages...")
	}

	return messageListStyle.Width(width).Height(height).Render(content)
}

func (m model) renderDetailPanel(width, height int) string {
	if len(m.messages) == 0 || m.selectedIdx >= len(m.messages) {
		return detailPanelStyle.Width(width).Height(height).Render(
			mutedStyle.Render("Select a message to view details"),
		)
	}

	msg := m.messages[m.selectedIdx]
	var content strings.Builder

	// Header info
	content.WriteString(fieldNameStyle.Render("Routing Key: "))
	content.WriteString(fieldValueStyle.Render(msg.RoutingKey))
	content.WriteString("\n")

	content.WriteString(fieldNameStyle.Render("Exchange: "))
	content.WriteString(fieldValueStyle.Render(msg.Exchange))
	content.WriteString("\n")

	content.WriteString(fieldNameStyle.Render("Timestamp: "))
	content.WriteString(fieldValueStyle.Render(msg.Timestamp.Format(time.RFC3339)))
	content.WriteString("\n")

	content.WriteString(fieldNameStyle.Render("Size: "))
	content.WriteString(fieldValueStyle.Render(fmt.Sprintf("%d bytes", len(msg.RawBody))))
	content.WriteString("\n\n")

	// Headers
	if len(msg.Headers) > 0 {
		content.WriteString(fieldNameStyle.Render("Headers:\n"))
		for k, v := range msg.Headers {
			content.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
		content.WriteString("\n")
	}

	// Body
	content.WriteString(fieldNameStyle.Render("Body:\n"))
	if m.showRaw {
		content.WriteString(fieldValueStyle.Render(formatHex(msg.RawBody, width-4)))
	} else if msg.DecodeErr != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Decode error: %v\n", msg.DecodeErr)))
		content.WriteString(fieldValueStyle.Render(formatHex(msg.RawBody, width-4)))
	} else if msg.Decoded != nil {
		content.WriteString(fieldValueStyle.Render(formatJSON(msg.Decoded, width-4)))
	} else {
		content.WriteString(fieldValueStyle.Render(formatHex(msg.RawBody, width-4)))
	}

	return detailPanelStyle.Width(width).Height(height).Render(content.String())
}

func (m model) renderHelpBar() string {
	keys := []struct{ key, desc string }{
		{"↑/k", "up"},
		{"↓/j", "down"},
		{"g/G", "top/bottom"},
		{"r", "raw/decoded"},
		{"p/space", "pause"},
		{"c", "clear"},
		{"b", "browser"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %s", helpKeyStyle.Render(k.key), k.desc))
	}

	return helpStyle.Render(strings.Join(parts, "  │  "))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatHex(data []byte, width int) string {
	var sb strings.Builder
	for i, b := range data {
		if i > 0 && i%16 == 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%02x ", b))
		if i > 500 {
			sb.WriteString("...")
			break
		}
	}
	return sb.String()
}

func formatJSON(data map[string]any, width int) string {
	var sb strings.Builder
	formatValue(&sb, data, 0, width)
	return sb.String()
}

func formatValue(sb *strings.Builder, v any, indent int, width int) {
	indentStr := strings.Repeat("  ", indent)

	switch val := v.(type) {
	case map[string]any:
		sb.WriteString("{\n")
		i := 0
		for k, v := range val {
			sb.WriteString(indentStr)
			sb.WriteString("  ")
			sb.WriteString(fieldNameStyle.Render(k))
			sb.WriteString(": ")
			formatValue(sb, v, indent+1, width)
			i++
			if i < len(val) {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(indentStr)
		sb.WriteString("}")
	case []any:
		sb.WriteString("[\n")
		for i, item := range val {
			sb.WriteString(indentStr)
			sb.WriteString("  ")
			formatValue(sb, item, indent+1, width)
			if i < len(val)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(indentStr)
		sb.WriteString("]")
	case string:
		sb.WriteString(fmt.Sprintf("%q", val))
	default:
		sb.WriteString(fmt.Sprintf("%v", val))
	}
}
