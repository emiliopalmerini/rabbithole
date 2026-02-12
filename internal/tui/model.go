package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/epalmerini/rabbithole/internal/db"
	"github.com/epalmerini/rabbithole/internal/rabbitmq"
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

	// Consumer lifecycle
	amqpConsumer  *rabbitmq.Consumer
	cancelConsume context.CancelFunc

	// Persistence (injected store + per-session state)
	store       db.Store
	asyncWriter *db.AsyncWriter
	sessionID   int64

	// Vim command state
	vimKeys VimKeyState

	// Search
	searchMode      bool
	searchQuery     string
	searchInput     textinput.Model
	searchResults   []int
	searchResultIdx int

	// Bookmarks
	bookmarks map[int]bool

	// UI state
	splitRatio   float64
	compactMode  bool
	showHelp     bool
	timestampRel bool

	// New messages indicator (when paused)
	newMsgCount int

	// Components
	spinner        spinner.Model
	detailViewport viewport.Model

	// Status messages (brief confirmations)
	statusMsg     string
	statusMsgTime time.Time
}

// Tea messages
type msgReceived struct {
	msg Message
}

type connectedMsg struct {
	msgChan         <-chan Message
	historicalMsgs  []Message
	historicalCount int
	consumer        *rabbitmq.Consumer
	cancelConsume   context.CancelFunc
	asyncWriter     *db.AsyncWriter
	sessionID       int64
}

type connectionErrorMsg struct {
	err error
}

type clearStatusMsg struct{}

func initialModel(cfg Config, store db.Store) model {
	si := textinput.New()
	si.Placeholder = "Search..."
	si.CharLimit = 100
	si.Width = 30

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	splitRatio := cfg.DefaultSplitRatio
	if splitRatio == 0 {
		splitRatio = 0.5
	}

	return model{
		config:         cfg,
		store:          store,
		messages:       make([]Message, 0, 1000),
		connState:      stateConnecting,
		viewport:       viewport.New(80, 20),
		detailViewport: viewport.New(80, 20),
		vimKeys:        NewVimKeyState(),
		bookmarks:      make(map[int]bool),
		splitRatio:     splitRatio,
		compactMode:    cfg.CompactMode,
		searchInput:    si,
		spinner:        sp,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.connectCmd(),
		m.spinner.Tick,
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle search mode input
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.searchQuery = ""
				m.searchResults = nil
				m.searchInput.Blur()
				return m, nil
			case "enter":
				m.searchMode = false
				m.searchQuery = m.searchInput.Value()
				m.searchInput.Blur()
				m.performSearch()
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		}

		// Handle help overlay
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
				m.showHelp = false
				return m, nil
			}
			return m, nil
		}

		// Handle special keys that bypass vim handler
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+u":
			m.moveBy(-m.visibleItems() / 2)
			return m, nil
		case "ctrl+d":
			m.moveBy(m.visibleItems() / 2)
			return m, nil
		case "ctrl+f":
			m.moveBy(m.visibleItems())
			return m, nil
		case "ctrl+b":
			m.moveBy(-m.visibleItems())
			return m, nil
		case "ctrl+j":
			// Scroll detail viewport down
			m.detailViewport.YOffset++
			return m, nil
		case "ctrl+k":
			// Scroll detail viewport up
			if m.detailViewport.YOffset > 0 {
				m.detailViewport.YOffset--
			}
			return m, nil
		case "up":
			m.moveBy(-1)
			return m, nil
		case "down":
			m.moveBy(1)
			return m, nil
		}

		// Process through vim key handler
		result := m.vimKeys.ProcessKey(msg.String())
		if result.Action == "pending" {
			return m, nil
		}

		switch result.Action {
		case "move_down":
			m.moveBy(result.Count)
		case "move_up":
			m.moveBy(-result.Count)
		case "go_top":
			m.selectedIdx = 0
			m.detailViewport.YOffset = 0
		case "go_bottom":
			if len(m.messages) > 0 {
				m.selectedIdx = len(m.messages) - 1
				m.detailViewport.YOffset = 0
			}
		case "center_line":
			// Centering is handled in renderMessageList
		case "search_start":
			m.searchMode = true
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, textinput.Blink
		case "search_next":
			m.nextSearchResult()
		case "search_prev":
			m.prevSearchResult()
		case "yank":
			return m, m.yankMessage()
		case "export":
			return m, m.exportMessages()
		case "bookmark_toggle":
			m.toggleBookmark()
		case "bookmark_next":
			m.nextBookmark()
		case "toggle_compact":
			m.compactMode = !m.compactMode
		case "toggle_timestamp":
			m.timestampRel = !m.timestampRel
		case "toggle_raw":
			m.showRaw = !m.showRaw
		case "toggle_help":
			m.showHelp = !m.showHelp
		case "resize_left":
			if m.splitRatio > 0.2 {
				m.splitRatio -= 0.05
			}
		case "resize_right":
			if m.splitRatio < 0.8 {
				m.splitRatio += 0.05
			}
		case "pause_toggle":
			m.paused = !m.paused
			if !m.paused {
				m.newMsgCount = 0
			}
		case "clear":
			m.messages = m.messages[:0]
			m.selectedIdx = 0
			m.messageCount = 0
			m.bookmarks = make(map[int]bool)
			m.newMsgCount = 0
		case "back":
			// Handled by parent app model
		case "quit":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Update viewport dimensions
		contentHeight := m.height - 5 // header(3) + status(1) + help(1)
		if contentHeight < 3 {
			contentHeight = 3
		}
		listWidth := int(float64(m.width) * m.splitRatio)
		if listWidth < 20 {
			listWidth = 20
		}
		detailWidth := m.width - listWidth - 1
		if detailWidth < 20 {
			detailWidth = 20
		}

		// Resize detail viewport (account for border and padding)
		m.detailViewport.Width = detailWidth - 4
		m.detailViewport.Height = contentHeight - 2

	case connectedMsg:
		m.connState = stateConnected
		m.msgChan = msg.msgChan
		m.amqpConsumer = msg.consumer
		m.cancelConsume = msg.cancelConsume
		m.asyncWriter = msg.asyncWriter
		m.sessionID = msg.sessionID
		// Load historical messages first
		if len(msg.historicalMsgs) > 0 {
			m.messages = msg.historicalMsgs
			m.messageCount = msg.historicalCount
		}
		cmds = append(cmds, m.waitForMessage())

	case connectionErrorMsg:
		m.connState = stateDisconnected
		m.connError = msg.err

	case retryMsg:
		m.connState = stateConnecting
		m.connError = nil
		cmds = append(cmds, scheduleRetry(msg.attempt, msg.delay))

	case retryTickMsg:
		cmds = append(cmds, m.connectWithRetry(msg.attempt))

	case msgReceived:
		if m.paused {
			m.newMsgCount++
		} else {
			m.messageCount++
			msg.msg.ID = m.messageCount
			m.messages = append(m.messages, msg.msg)
			// Keep max 1000 messages
			if len(m.messages) > 1000 {
				// Update bookmarks when removing old messages
				newBookmarks := make(map[int]bool)
				for id := range m.bookmarks {
					if id > 1 {
						newBookmarks[id-1] = true
					}
				}
				m.bookmarks = newBookmarks
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

	case spinner.TickMsg:
		// Only tick spinner when connecting
		if m.connState == stateConnecting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case clearStatusMsg:
		m.statusMsg = ""
	}

	return m, tea.Batch(cmds...)
}

func (m *model) moveBy(delta int) {
	newIdx := m.selectedIdx + delta
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx >= len(m.messages) {
		newIdx = len(m.messages) - 1
	}
	if newIdx < 0 {
		newIdx = 0
	}

	// Reset detail scroll when selection changes
	if newIdx != m.selectedIdx {
		m.detailViewport.YOffset = 0
	}

	m.selectedIdx = newIdx

	// Auto-pause on select if configured
	if m.config.AutoPauseOnSelect && delta != 0 {
		m.paused = true
	}
}

func (m model) visibleItems() int {
	// Account for borders (2) in message list
	items := m.height - 6
	if items < 1 {
		return 1
	}
	return items
}

func (m *model) performSearch() {
	m.searchResults = nil
	m.searchResultIdx = 0
	if m.searchQuery == "" {
		return
	}

	query := strings.ToLower(m.searchQuery)
	for i, msg := range m.messages {
		// Search in routing key
		if strings.Contains(strings.ToLower(msg.RoutingKey), query) {
			m.searchResults = append(m.searchResults, i)
			continue
		}
		// Search in decoded body
		if msg.Decoded != nil {
			bodyJSON, _ := json.Marshal(msg.Decoded)
			if strings.Contains(strings.ToLower(string(bodyJSON)), query) {
				m.searchResults = append(m.searchResults, i)
			}
		}
	}

	// Jump to first result
	if len(m.searchResults) > 0 {
		m.selectedIdx = m.searchResults[0]
		m.detailViewport.YOffset = 0
	}
}

func (m *model) nextSearchResult() {
	if len(m.searchResults) == 0 {
		return
	}
	m.searchResultIdx = (m.searchResultIdx + 1) % len(m.searchResults)
	m.selectedIdx = m.searchResults[m.searchResultIdx]
	m.detailViewport.YOffset = 0
}

func (m *model) prevSearchResult() {
	if len(m.searchResults) == 0 {
		return
	}
	m.searchResultIdx--
	if m.searchResultIdx < 0 {
		m.searchResultIdx = len(m.searchResults) - 1
	}
	m.selectedIdx = m.searchResults[m.searchResultIdx]
	m.detailViewport.YOffset = 0
}

func (m *model) toggleBookmark() {
	if len(m.messages) == 0 {
		return
	}
	msgID := m.messages[m.selectedIdx].ID
	if m.bookmarks[msgID] {
		delete(m.bookmarks, msgID)
	} else {
		m.bookmarks[msgID] = true
	}
}

func (m *model) nextBookmark() {
	if len(m.bookmarks) == 0 {
		return
	}

	// Find next bookmarked message after current position
	for i := m.selectedIdx + 1; i < len(m.messages); i++ {
		if m.bookmarks[m.messages[i].ID] {
			m.selectedIdx = i
			m.detailViewport.YOffset = 0
			return
		}
	}
	// Wrap around
	for i := 0; i <= m.selectedIdx; i++ {
		if m.bookmarks[m.messages[i].ID] {
			m.selectedIdx = i
			m.detailViewport.YOffset = 0
			return
		}
	}
}

func (m *model) yankMessage() tea.Cmd {
	if len(m.messages) == 0 || m.selectedIdx >= len(m.messages) {
		return nil
	}

	msg := m.messages[m.selectedIdx]

	type yankMessage struct {
		RoutingKey string         `json:"routing_key"`
		Exchange   string         `json:"exchange"`
		Timestamp  time.Time      `json:"timestamp"`
		Headers    map[string]any `json:"headers,omitempty"`
		Body       any            `json:"body"`
	}

	yank := yankMessage{
		RoutingKey: msg.RoutingKey,
		Exchange:   msg.Exchange,
		Timestamp:  msg.Timestamp,
		Headers:    msg.Headers,
	}

	if msg.Decoded != nil {
		yank.Body = msg.Decoded
	} else {
		yank.Body = base64.StdEncoding.EncodeToString(msg.RawBody)
	}

	content, _ := json.MarshalIndent(yank, "", "  ")

	if err := clipboard.WriteAll(string(content)); err != nil {
		return m.setStatusMsg("Copy failed: " + err.Error())
	}
	return m.setStatusMsg("Copied to clipboard")
}

func (m *model) exportMessages() tea.Cmd {
	if len(m.messages) == 0 {
		return m.setStatusMsg("No messages to export")
	}

	type exportMessage struct {
		ID         int            `json:"id"`
		RoutingKey string         `json:"routing_key"`
		Exchange   string         `json:"exchange"`
		Timestamp  time.Time      `json:"timestamp"`
		Headers    map[string]any `json:"headers,omitempty"`
		Body       any            `json:"body,omitempty"`
		RawBody    string         `json:"raw_body"`
	}

	exports := make([]exportMessage, len(m.messages))
	for i, msg := range m.messages {
		exports[i] = exportMessage{
			ID:         msg.ID,
			RoutingKey: msg.RoutingKey,
			Exchange:   msg.Exchange,
			Timestamp:  msg.Timestamp,
			Headers:    msg.Headers,
			Body:       msg.Decoded,
			RawBody:    base64.StdEncoding.EncodeToString(msg.RawBody),
		}
	}

	filename := fmt.Sprintf("rabbithole-export-%s.json", time.Now().Format("20060102-150405"))
	data, _ := json.MarshalIndent(exports, "", "  ")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}
	return m.setStatusMsg(fmt.Sprintf("Exported to %s", filename))
}

func (m *model) setStatusMsg(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusMsgTime = time.Now()
	return tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m model) View() string {
	if m.width == 0 {
		return m.spinner.View() + " Loading..."
	}

	// Help overlay
	if m.showHelp {
		return m.renderHelpOverlay()
	}

	// Calculate content height: total - header(3) - status(1) - help(1)
	contentHeight := m.height - 5
	if contentHeight < 3 {
		contentHeight = 3
	}

	// Calculate widths
	listWidth := int(float64(m.width) * m.splitRatio)
	if listWidth < 20 {
		listWidth = 20
	}
	detailWidth := m.width - listWidth - 1
	if detailWidth < 20 {
		detailWidth = 20
	}

	// Header
	header := headerStyle.Width(m.width - 2).Render("rabbithole")

	// Status bar
	status := m.renderStatusBar()

	// Main content
	messageList := m.renderMessageList(listWidth, contentHeight)
	detailPanel := m.renderDetailPanel(detailWidth, contentHeight)

	content := lipgloss.JoinHorizontal(lipgloss.Top, messageList, detailPanel)

	// Help bar or search bar
	var bottomBar string
	if m.searchMode {
		bottomBar = m.renderSearchBar()
	} else {
		bottomBar = m.renderHelpBar()
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, status, content, bottomBar)
}

func (m model) renderStatusBar() string {
	var connStatus string
	switch m.connState {
	case stateConnected:
		connStatus = connectedStyle.Render("● Connected")
	case stateConnecting:
		connStatus = statusBarStyle.Render(m.spinner.View() + " Connecting...")
	default:
		errMsg := ""
		if m.connError != nil {
			errMsg = fmt.Sprintf(" (%s)", m.connError.Error())
		}
		connStatus = disconnectedStyle.Render("○ Disconnected" + errMsg)
	}

	exchange := statusBarStyle.Render(fmt.Sprintf("Exchange: %s", m.config.Exchange))
	routingKey := statusBarStyle.Render(fmt.Sprintf("Routing: %s", m.config.RoutingKey))

	// Count historical vs live messages
	historicalCount := 0
	for _, msg := range m.messages {
		if msg.Historical {
			historicalCount++
		}
	}
	liveCount := len(m.messages) - historicalCount

	var msgCount string
	if historicalCount > 0 {
		msgCount = statusBarStyle.Render(fmt.Sprintf("Messages: %dH+%dL", historicalCount, liveCount))
	} else {
		msgCount = statusBarStyle.Render(fmt.Sprintf("Messages: %d", len(m.messages)))
	}

	pausedStatus := ""
	if m.paused {
		pausedStatus = disconnectedStyle.Render(" [PAUSED]")
		if m.newMsgCount > 0 {
			pausedStatus += " " + newMsgStyle.Render(fmt.Sprintf("+%d new", m.newMsgCount))
		}
	}

	// Search results indicator
	searchStatus := ""
	if m.searchQuery != "" {
		if len(m.searchResults) > 0 {
			searchStatus = statusBarStyle.Render(fmt.Sprintf(" [%d/%d]", m.searchResultIdx+1, len(m.searchResults)))
		} else {
			searchStatus = mutedStyle.Render(" (no matches)")
		}
	}

	// Status message (brief confirmation)
	statusMsgDisplay := ""
	if m.statusMsg != "" && time.Since(m.statusMsgTime) < 3*time.Second {
		statusMsgDisplay = "  " + confirmationStyle.Render(m.statusMsg)
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		connStatus,
		pausedStatus,
		searchStatus,
		statusMsgDisplay,
		"  │  ",
		exchange,
		"  │  ",
		routingKey,
		"  │  ",
		msgCount,
	)
}

func (m model) renderMessageList(width, height int) string {
	// Account for border (2 lines)
	innerHeight := height - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Empty state
	if len(m.messages) == 0 {
		emptyContent := strings.Join([]string{
			"",
			emptyStateStyle.Render("No messages yet"),
			"",
			mutedStyle.Render(fmt.Sprintf("Watching: %s", m.config.Exchange)),
			mutedStyle.Render(fmt.Sprintf("Routing: %s", m.config.RoutingKey)),
			"",
			mutedStyle.Render("Press ? for help"),
		}, "\n")
		return messageListStyle.Width(width).Height(height).Render(emptyContent)
	}

	startIdx := 0
	if m.selectedIdx >= innerHeight {
		startIdx = m.selectedIdx - innerHeight + 1
	}

	endIdx := startIdx + innerHeight
	if endIdx > len(m.messages) {
		endIdx = len(m.messages)
	}

	items := make([]string, 0, innerHeight)
	innerWidth := width - 4 // Account for border and padding

	for i := startIdx; i < endIdx; i++ {
		msg := m.messages[i]

		// Source indicator: H=historical (from DB), L=live (from queue)
		sourceIndicator := "L"
		if msg.Historical {
			sourceIndicator = "H"
		}

		// Bookmark indicator
		prefix := sourceIndicator + " "
		if m.bookmarks[msg.ID] {
			prefix = sourceIndicator + "*"
		}
		if i == m.selectedIdx {
			prefix = sourceIndicator + ">"
		}

		var line string
		if m.compactMode {
			rk := truncate(msg.RoutingKey, innerWidth-3)
			line = prefix + rk
		} else {
			var ts string
			if m.timestampRel {
				ts = formatRelativeTime(msg.Timestamp)
			} else {
				ts = msg.Timestamp.Format("15:04:05")
			}
			rk := truncate(msg.RoutingKey, innerWidth-12)
			line = fmt.Sprintf("%s%s %s", prefix, ts, rk)
		}

		if i == m.selectedIdx {
			line = selectedMessageStyle.Render(line)
		} else if m.bookmarks[msg.ID] {
			line = bookmarkStyle.Render(line)
		} else if msg.Historical {
			line = mutedStyle.Render(line)
		}

		items = append(items, line)
	}

	content := strings.Join(items, "\n")
	return messageListStyle.Width(width).Height(height).Render(content)
}

func (m model) renderDetailPanel(width, height int) string {
	// Account for border (2 lines)
	innerHeight := height - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	if len(m.messages) == 0 || m.selectedIdx >= len(m.messages) {
		return detailPanelStyle.Width(width).Height(height).Render(
			mutedStyle.Render("Select a message to view details"),
		)
	}

	msg := m.messages[m.selectedIdx]
	innerWidth := width - 4
	var lines []string

	// METADATA section
	lines = append(lines, fieldNameStyle.Render("METADATA"))
	lines = append(lines, dividerStyle.Render(strings.Repeat("─", innerWidth)))
	lines = append(lines, fieldNameStyle.Render("Routing Key: ")+msg.RoutingKey)
	lines = append(lines, fieldNameStyle.Render("Exchange: ")+msg.Exchange)
	lines = append(lines, fieldNameStyle.Render("Timestamp: ")+msg.Timestamp.Format(time.RFC3339))
	lines = append(lines, fieldNameStyle.Render("Size: ")+fmt.Sprintf("%d bytes", len(msg.RawBody)))
	lines = append(lines, "")

	// HEADERS section
	if len(msg.Headers) > 0 {
		lines = append(lines, fieldNameStyle.Render("HEADERS"))
		lines = append(lines, dividerStyle.Render(strings.Repeat("─", innerWidth)))
		// Sort header keys for stable output
		headerKeys := make([]string, 0, len(msg.Headers))
		for k := range msg.Headers {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)
		for _, k := range headerKeys {
			lines = append(lines, fmt.Sprintf("%s: %s", fieldNameStyle.Render(k), formatHeaderValue(msg.Headers[k])))
		}
		lines = append(lines, "")
	}

	// BODY section
	lines = append(lines, fieldNameStyle.Render("BODY"))
	lines = append(lines, dividerStyle.Render(strings.Repeat("─", innerWidth)))

	if m.showRaw {
		lines = append(lines, formatHex(msg.RawBody))
	} else if msg.DecodeErr != nil {
		lines = append(lines, errorStyle.Render(fmt.Sprintf("Decode error: %v", msg.DecodeErr)))
		lines = append(lines, formatHex(msg.RawBody))
	} else if msg.Decoded != nil {
		lines = append(lines, formatJSONSyntax(msg.Decoded))
	} else {
		lines = append(lines, formatHex(msg.RawBody))
	}

	// Split into individual lines for scrolling
	allLines := strings.Split(strings.Join(lines, "\n"), "\n")

	// Apply scroll offset from viewport
	scrollOffset := m.detailViewport.YOffset
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	if scrollOffset > len(allLines)-innerHeight {
		scrollOffset = len(allLines) - innerHeight
		if scrollOffset < 0 {
			scrollOffset = 0
		}
	}

	// Get visible lines
	endIdx := scrollOffset + innerHeight
	if endIdx > len(allLines) {
		endIdx = len(allLines)
	}

	visibleLines := allLines[scrollOffset:endIdx]

	// Pad to fill height if content is shorter
	for len(visibleLines) < innerHeight {
		visibleLines = append(visibleLines, "")
	}

	content := strings.Join(visibleLines, "\n")
	return detailPanelStyle.Width(width).Height(height).Render(content)
}

func (m model) renderSearchBar() string {
	return helpStyle.Render("Search: ") + m.searchInput.View() + helpStyle.Render("  (Enter to search, Esc to cancel)")
}

func (m model) renderHelpBar() string {
	keys := []struct{ key, desc string }{
		{"j/k", "nav"},
		{"gg/G", "top/end"},
		{"/", "search"},
		{"y", "copy"},
		{"m", "mark"},
		{"r", "raw"},
		{"p", "pause"},
		{"?", "help"},
		{"b", "back"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, helpKeyStyle.Render(k.key)+" "+k.desc)
	}

	return helpStyle.Render(strings.Join(parts, " │ "))
}

func (m model) renderHelpOverlay() string {
	var lines []string

	lines = append(lines, fieldNameStyle.Render("Keybindings"))
	lines = append(lines, "")

	sections := []struct {
		name string
		keys []struct{ key, desc string }
	}{
		{
			name: "Navigation",
			keys: []struct{ key, desc string }{
				{"j / k", "Move down / up"},
				{"5j / 10k", "Move 5 down / 10 up"},
				{"gg", "Go to top"},
				{"G", "Go to bottom"},
				{"Ctrl+U / Ctrl+D", "Half page up / down"},
				{"Ctrl+F / Ctrl+B", "Full page up / down"},
			},
		},
		{
			name: "Search",
			keys: []struct{ key, desc string }{
				{"/", "Start search"},
				{"n / N", "Next / previous result"},
				{"Esc", "Clear search"},
			},
		},
		{
			name: "Actions",
			keys: []struct{ key, desc string }{
				{"y", "Copy message to clipboard"},
				{"e", "Export all messages to JSON"},
				{"m", "Toggle bookmark"},
				{"'", "Jump to next bookmark"},
				{"c", "Clear all messages"},
			},
		},
		{
			name: "View",
			keys: []struct{ key, desc string }{
				{"r", "Toggle raw/decoded view"},
				{"t", "Toggle compact mode"},
				{"T", "Toggle timestamp format"},
				{"H / L", "Resize panes left / right"},
				{"?", "Toggle this help"},
			},
		},
		{
			name: "Control",
			keys: []struct{ key, desc string }{
				{"p / Space", "Pause / resume"},
				{"b", "Back to browser"},
				{"q / Ctrl+C", "Quit"},
			},
		},
	}

	for _, section := range sections {
		lines = append(lines, helpCategoryStyle.Render(section.name))
		for _, k := range section.keys {
			lines = append(lines, fmt.Sprintf("  %-18s %s", helpKeyStyle.Render(k.key), k.desc))
		}
		lines = append(lines, "")
	}

	lines = append(lines, mutedStyle.Render("Press ? or Esc to close"))

	content := strings.Join(lines, "\n")

	overlayWidth := 50
	overlayHeight := len(lines) + 4
	if overlayHeight > m.height-4 {
		overlayHeight = m.height - 4
	}

	overlay := helpOverlayStyle.Width(overlayWidth).Render(content)

	// Center the overlay
	hPad := (m.width - overlayWidth) / 2
	vPad := (m.height - overlayHeight) / 2
	if hPad < 0 {
		hPad = 0
	}
	if vPad < 0 {
		vPad = 0
	}

	return lipgloss.NewStyle().
		PaddingLeft(hPad).
		PaddingTop(vPad).
		Render(overlay)
}

func truncate(s string, max int) string {
	if max <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

func formatHex(data []byte) string {
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

func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// formatHeaderValue formats a header value as JSON for complex types, or as a simple string for primitives
func formatHeaderValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", val)
	case nil:
		return "null"
	default:
		// For maps, slices, and other complex types, marshal as JSON
		if jsonBytes, err := json.Marshal(val); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("%v", val)
	}
}

// formatJSONSyntax formats JSON with syntax highlighting
func formatJSONSyntax(data map[string]any) string {
	var sb strings.Builder
	formatValueSyntax(&sb, data, 0)
	return sb.String()
}

func formatValueSyntax(sb *strings.Builder, v any, indent int) {
	indentStr := strings.Repeat("  ", indent)

	switch val := v.(type) {
	case map[string]any:
		sb.WriteString("{\n")
		// Sort keys for stable output
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			sb.WriteString(indentStr)
			sb.WriteString("  ")
			sb.WriteString(jsonKeyStyle.Render(fmt.Sprintf("%q", k)))
			sb.WriteString(": ")
			formatValueSyntax(sb, val[k], indent+1)
			if i < len(keys)-1 {
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
			formatValueSyntax(sb, item, indent+1)
			if i < len(val)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString(indentStr)
		sb.WriteString("]")
	case string:
		sb.WriteString(jsonStringStyle.Render(fmt.Sprintf("%q", val)))
	case float64:
		sb.WriteString(jsonNumberStyle.Render(fmt.Sprintf("%v", val)))
	case int:
		sb.WriteString(jsonNumberStyle.Render(fmt.Sprintf("%d", val)))
	case bool:
		sb.WriteString(jsonBoolStyle.Render(fmt.Sprintf("%v", val)))
	case nil:
		sb.WriteString(jsonNullStyle.Render("null"))
	default:
		// For unknown types (including maps not matching map[string]any), marshal as JSON
		if jsonBytes, err := json.Marshal(val); err == nil {
			sb.WriteString(string(jsonBytes))
		} else {
			fmt.Fprintf(sb, "%v", val)
		}
	}
}
