package tui

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	// Replay mode (read-only, no AMQP connection)
	replayMode bool

	// Vim command state
	vimKeys VimKeyState

	// Search
	searchMode      bool
	searchQuery     string
	searchInput     textinput.Model
	searchResults   []int
	searchResultIdx int

	// Filter
	filterMode   bool
	filterExpr   string
	filterActive bool
	filterInput  textinput.Model
	filteredIdx  []int // sorted indices into m.messages; nil when filter is off

	// Bookmarks
	bookmarks map[int]bool

	// UI state
	splitRatio   float64
	compactMode  bool
	showHelp     bool
	timestampRel bool
	detailTab    int // 0=metadata, 1=headers, 2=body

	// Pause buffer (messages received while paused)
	pauseBuffer []Message
	newMsgCount int

	// Components
	spinner        spinner.Model
	detailViewport viewport.Model

	// Live stats
	stats stats

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

	fi := textinput.New()
	fi.Placeholder = "Filter..."
	fi.CharLimit = 100
	fi.Width = 30

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	splitRatio := loadSplitRatio(cfg.DefaultSplitRatio)

	return model{
		config:         cfg,
		store:          store,
		messages:       make([]Message, 0, cfg.MessageLimit()),
		connState:      stateConnecting,
		viewport:       viewport.New(80, 20),
		detailViewport: viewport.New(80, 20),
		vimKeys:        NewVimKeyState(),
		bookmarks:      make(map[int]bool),
		splitRatio:     splitRatio,
		compactMode:    cfg.CompactMode,
		searchInput:    si,
		filterInput:    fi,
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

		// Handle filter mode input
		if m.filterMode {
			switch msg.String() {
			case "esc":
				m.filterMode = false
				m.filterInput.Blur()
				return m, nil
			case "enter":
				m.filterMode = false
				m.filterInput.Blur()
				expr := m.filterInput.Value()
				if expr == "" {
					// Clear filter
					m.filterExpr = ""
					m.filterActive = false
					m.filteredIdx = nil
				} else {
					m.filterExpr = expr
					m.filterActive = true
					m.filteredIdx = computeFilteredIndices(m.messages, expr)
					if len(m.filteredIdx) > 0 && !isVisible(m.filteredIdx, m.selectedIdx) {
						m.selectedIdx = m.filteredIdx[0]
						m.detailViewport.YOffset = 0
					}
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
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
		case "tab":
			m.detailTab = (m.detailTab + 1) % m.tabCount()
			m.detailViewport.YOffset = 0
			return m, nil
		case "shift+tab":
			m.detailTab = (m.detailTab + m.tabCount() - 1) % m.tabCount()
			m.detailViewport.YOffset = 0
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
			if len(m.filteredIdx) > 0 {
				m.selectedIdx = m.filteredIdx[0]
			} else {
				m.selectedIdx = 0
			}
			m.detailViewport.YOffset = 0
		case "go_bottom":
			if len(m.filteredIdx) > 0 {
				m.selectedIdx = m.filteredIdx[len(m.filteredIdx)-1]
			} else if len(m.messages) > 0 {
				m.selectedIdx = len(m.messages) - 1
			}
			m.detailViewport.YOffset = 0
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
		case "filter_start":
			m.filterMode = true
			m.filterInput.SetValue(m.filterExpr)
			m.filterInput.Focus()
			return m, textinput.Blink
		case "filter_toggle":
			if m.filterExpr != "" {
				m.filterActive = !m.filterActive
				if m.filterActive {
					m.filteredIdx = computeFilteredIndices(m.messages, m.filterExpr)
					if len(m.filteredIdx) > 0 && !isVisible(m.filteredIdx, m.selectedIdx) {
						m.selectedIdx = m.filteredIdx[0]
						m.detailViewport.YOffset = 0
					}
				} else {
					m.filteredIdx = nil
				}
			}
		case "yank":
			return m, m.yankMessage()
		case "yank_tab":
			return m, m.yankTab()
		case "export":
			return m, m.exportMessages()
		case "export_csv":
			return m, m.exportCSV()
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
				saveSplitRatio(m.splitRatio)
			}
		case "resize_right":
			if m.splitRatio < 0.8 {
				m.splitRatio += 0.05
				saveSplitRatio(m.splitRatio)
			}
		case "pause_toggle":
			if m.replayMode {
				return m, nil
			}
			m.paused = !m.paused
			if !m.paused {
				// Merge buffered messages into the main list
				for _, buffered := range m.pauseBuffer {
					m.messageCount++
					buffered.ID = m.messageCount
					m.messages = append(m.messages, buffered)
					if len(m.messages) > m.config.MessageLimit() {
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
				}
				m.pauseBuffer = m.pauseBuffer[:0]
				m.newMsgCount = 0
				// Re-apply filter after merging
				if m.filterActive && m.filterExpr != "" {
					m.filteredIdx = computeFilteredIndices(m.messages, m.filterExpr)
				}
			}
		case "clear":
			m.messages = m.messages[:0]
			m.pauseBuffer = m.pauseBuffer[:0]
			m.selectedIdx = 0
			m.messageCount = 0
			m.bookmarks = make(map[int]bool)
			m.filteredIdx = nil
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

	case connectionLostMsg:
		m.cleanup()
		m.connState = stateConnecting
		m.connError = nil
		cmds = append(cmds, m.spinner.Tick, scheduleRetry(0, initialBackoff))

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
		m.stats.record(time.Now(), len(msg.msg.RawBody))
		if m.paused {
			m.newMsgCount++
			msg.msg.ID = m.messageCount + m.newMsgCount
			m.pauseBuffer = append(m.pauseBuffer, msg.msg)
			// Cap pause buffer
			if len(m.pauseBuffer) > m.config.MessageLimit() {
				m.pauseBuffer = m.pauseBuffer[1:]
			}
		} else {
			m.messageCount++
			msg.msg.ID = m.messageCount
			m.messages = append(m.messages, msg.msg)
			// Keep max messages
			if len(m.messages) > m.config.MessageLimit() {
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
		// Re-apply filter for new message
		if m.filterActive && m.filterExpr != "" {
			m.filteredIdx = computeFilteredIndices(m.messages, m.filterExpr)
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
	var newIdx int

	if len(m.filteredIdx) > 0 {
		// Navigate through visible messages only
		if delta > 0 {
			newIdx = m.selectedIdx
			for i := 0; i < delta; i++ {
				newIdx = nextVisible(m.filteredIdx, newIdx)
			}
		} else {
			newIdx = m.selectedIdx
			for i := 0; i < -delta; i++ {
				newIdx = prevVisible(m.filteredIdx, newIdx)
			}
		}
	} else {
		newIdx = m.selectedIdx + delta
		if newIdx < 0 {
			newIdx = 0
		}
		if newIdx >= len(m.messages) {
			newIdx = len(m.messages) - 1
		}
		if newIdx < 0 {
			newIdx = 0
		}
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

	// Parse field prefix (e.g. "rk:country", "body:alice", "re:pattern")
	field, query := parseSearchQuery(m.searchQuery)

	var re *regexp.Regexp
	if field == "re" {
		var err error
		re, err = compileSearchRegex(query)
		if err != nil {
			m.setStatusMsg("Invalid regex: " + err.Error())
			return
		}
	} else {
		query = strings.ToLower(query)
	}

	for i, msg := range m.messages {
		if matchesSearch(msg, field, query, re) {
			m.searchResults = append(m.searchResults, i)
		}
	}

	// Jump to first result
	if len(m.searchResults) > 0 {
		m.selectedIdx = m.searchResults[0]
		m.detailViewport.YOffset = 0
	}
}

// parseSearchQuery extracts an optional field prefix from a search query.
// Supported prefixes: rk:, body:, ex:, hdr:, type:, re:
// Returns ("", query) for unprefixed queries.
func parseSearchQuery(q string) (field, query string) {
	for _, prefix := range []string{"rk:", "body:", "ex:", "hdr:", "type:", "re:"} {
		if strings.HasPrefix(q, prefix) {
			return prefix[:len(prefix)-1], q[len(prefix):]
		}
	}
	return "", q
}

// compileSearchRegex compiles a regex pattern for search.
func compileSearchRegex(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}

func matchesSearch(msg Message, field, query string, re *regexp.Regexp) bool {
	switch field {
	case "re":
		if re == nil {
			return false
		}
		if re.MatchString(msg.RoutingKey) {
			return true
		}
		if msg.Decoded != nil {
			bodyJSON, _ := json.Marshal(msg.Decoded)
			return re.Match(bodyJSON)
		}
		return false
	case "rk":
		return strings.Contains(strings.ToLower(msg.RoutingKey), query)
	case "body":
		if msg.Decoded != nil {
			bodyJSON, _ := json.Marshal(msg.Decoded)
			return strings.Contains(strings.ToLower(string(bodyJSON)), query)
		}
		return false
	case "ex":
		return strings.Contains(strings.ToLower(msg.Exchange), query)
	case "hdr":
		if len(msg.Headers) > 0 {
			hdrJSON, _ := json.Marshal(msg.Headers)
			return strings.Contains(strings.ToLower(string(hdrJSON)), query)
		}
		return false
	case "type":
		return strings.Contains(strings.ToLower(msg.ProtoType), query)
	default:
		// Unprefixed: search routing key + body (original behavior)
		if strings.Contains(strings.ToLower(msg.RoutingKey), query) {
			return true
		}
		if msg.Decoded != nil {
			bodyJSON, _ := json.Marshal(msg.Decoded)
			return strings.Contains(strings.ToLower(string(bodyJSON)), query)
		}
		return false
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

func (m *model) yankTab() tea.Cmd {
	if len(m.messages) == 0 || m.selectedIdx >= len(m.messages) {
		return nil
	}
	msg := m.messages[m.selectedIdx]

	switch m.detailTab {
	case 0: // Metadata → routing key
		if err := clipboard.WriteAll(msg.RoutingKey); err != nil {
			return m.setStatusMsg("Copy failed: " + err.Error())
		}
		return m.setStatusMsg("Copied routing key")
	case 1: // Headers
		if len(msg.Headers) == 0 {
			return m.setStatusMsg("No headers to copy")
		}
		content, _ := json.MarshalIndent(msg.Headers, "", "  ")
		if err := clipboard.WriteAll(string(content)); err != nil {
			return m.setStatusMsg("Copy failed: " + err.Error())
		}
		return m.setStatusMsg("Copied headers")
	case 2: // Body
		var body string
		if msg.Decoded != nil {
			b, _ := json.MarshalIndent(msg.Decoded, "", "  ")
			body = string(b)
		} else {
			body = string(msg.RawBody)
		}
		if err := clipboard.WriteAll(body); err != nil {
			return m.setStatusMsg("Copy failed: " + err.Error())
		}
		return m.setStatusMsg("Copied body")
	case 3: // Dead Letter
		lines := renderDLXTab(msg)
		content := strings.Join(lines, "\n")
		if err := clipboard.WriteAll(content); err != nil {
			return m.setStatusMsg("Copy failed: " + err.Error())
		}
		return m.setStatusMsg("Copied dead letter info")
	}
	return nil
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

	data, err := json.MarshalIndent(exports, "", "  ")
	if err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}

	// Write to XDG data directory
	dataDir, err := db.DefaultDataDir()
	if err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}
	exportDir := filepath.Join(dataDir, "exports")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}

	filename := fmt.Sprintf("rabbithole-export-%s.json", time.Now().Format("20060102-150405"))
	exportPath := filepath.Join(exportDir, filename)
	if err := os.WriteFile(exportPath, data, 0644); err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}
	return m.setStatusMsg(fmt.Sprintf("Exported to %s", exportPath))
}

func (m *model) exportCSV() tea.Cmd {
	if len(m.messages) == 0 {
		return m.setStatusMsg("No messages to export")
	}

	dataDir, err := db.DefaultDataDir()
	if err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}

	exportPath, err := writeCSVExport(m.messages, filepath.Join(dataDir, "exports"))
	if err != nil {
		return m.setStatusMsg("Export failed: " + err.Error())
	}
	return m.setStatusMsg(fmt.Sprintf("Exported to %s", exportPath))
}

func writeCSVExport(msgs []Message, exportDir string) (string, error) {
	if len(msgs) == 0 {
		return "", fmt.Errorf("no messages to export")
	}

	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("rabbithole-export-%s.csv", time.Now().Format("20060102-150405"))
	exportPath := filepath.Join(exportDir, filename)

	f, err := os.Create(exportPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// Header
	if err := w.Write([]string{"id", "timestamp", "exchange", "routing_key", "headers", "body"}); err != nil {
		return "", err
	}

	for _, msg := range msgs {
		var headers string
		if len(msg.Headers) > 0 {
			h, _ := json.Marshal(msg.Headers)
			headers = string(h)
		}

		var body string
		if msg.Decoded != nil {
			b, _ := json.MarshalIndent(msg.Decoded, "", "  ")
			body = string(b)
		} else {
			body = string(msg.RawBody)
		}

		record := []string{
			fmt.Sprintf("%d", msg.ID),
			msg.Timestamp.Format(time.RFC3339),
			msg.Exchange,
			msg.RoutingKey,
			headers,
			body,
		}
		if err := w.Write(record); err != nil {
			return "", err
		}
	}

	return exportPath, nil
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

	// Help bar, search bar, or filter bar
	var bottomBar string
	if m.searchMode {
		bottomBar = m.renderSearchBar()
	} else if m.filterMode {
		bottomBar = m.renderFilterBar()
	} else {
		bottomBar = m.renderHelpBar()
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, status, content, bottomBar)
}

func (m model) renderStatusBar() string {
	var connStatus string
	if m.replayMode {
		connStatus = connectedStyle.Render("▶ Replay")
	} else {
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

	// Filter indicator
	filterStatus := ""
	if m.filterActive && m.filterExpr != "" {
		filterStatus = disconnectedStyle.Render(fmt.Sprintf(" [FILTER: %s (%d)]", m.filterExpr, len(m.filteredIdx)))
	} else if m.filterExpr != "" {
		filterStatus = mutedStyle.Render(fmt.Sprintf(" [filter off: %s]", m.filterExpr))
	}

	// Status message (brief confirmation)
	statusMsgDisplay := ""
	if m.statusMsg != "" && time.Since(m.statusMsgTime) < 3*time.Second {
		statusMsgDisplay = "  " + confirmationStyle.Render(m.statusMsg)
	}

	// Live stats (only when connected and have messages)
	statsDisplay := ""
	if m.stats.totalMessages > 0 && !m.replayMode {
		rate := m.stats.msgPerSec(time.Now())
		avg := m.stats.avgSize()
		statsDisplay = mutedStyle.Render(fmt.Sprintf("%s  avg %s", formatRate(rate), formatBytes(avg)))
	}

	parts := []string{
		connStatus,
		pausedStatus,
		filterStatus,
		searchStatus,
		statusMsgDisplay,
		"  │  ",
		exchange,
		"  │  ",
		routingKey,
		"  │  ",
		msgCount,
	}
	if statsDisplay != "" {
		parts = append(parts, "  │  ", statsDisplay)
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
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

	// Build list of visible message indices
	var visible []int
	if len(m.filteredIdx) > 0 {
		visible = m.filteredIdx
	} else {
		visible = make([]int, len(m.messages))
		for i := range visible {
			visible[i] = i
		}
	}

	if len(visible) == 0 {
		emptyContent := emptyStateStyle.Render("No matches for filter")
		return messageListStyle.Width(width).Height(height).Render(emptyContent)
	}

	// Find selected position in visible list
	selPos := sort.SearchInts(visible, m.selectedIdx)
	if selPos >= len(visible) {
		selPos = len(visible) - 1
	}

	startPos := 0
	if selPos >= innerHeight {
		startPos = selPos - innerHeight + 1
	}

	endPos := startPos + innerHeight
	if endPos > len(visible) {
		endPos = len(visible)
	}

	items := make([]string, 0, innerHeight)
	innerWidth := width - 4 // Account for border and padding

	for _, i := range visible[startPos:endPos] {
		msg := m.messages[i]

		// Source indicator: H=historical (from DB), L=live (from queue)
		sourceIndicator := "L"
		if msg.Historical {
			sourceIndicator = "H"
		}

		// DLX indicator
		dlxIndicator := " "
		if isDLXMessage(msg) {
			dlxIndicator = "†"
		}

		// Bookmark indicator
		prefix := sourceIndicator + dlxIndicator
		if m.bookmarks[msg.ID] {
			prefix += "*"
		} else if i == m.selectedIdx {
			prefix += ">"
		} else {
			prefix += " "
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
		} else if isDLXMessage(msg) {
			line = dlxStyle.Render(line)
		} else if msg.Historical {
			line = mutedStyle.Render(line)
		}

		items = append(items, line)
	}

	content := strings.Join(items, "\n")
	return messageListStyle.Width(width).Height(height).Render(content)
}

func (m model) renderDetailPanel(width, height int) string {
	// Account for border (2 lines) and tab bar (1 line + divider)
	innerHeight := height - 4
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

	// Clamp tab index if DLX tab disappeared (switched to non-DLX message)
	if m.detailTab >= m.tabCount() {
		m.detailTab = 0
	}

	// Tab bar
	tabBar := m.renderDetailTabBar()

	// Render active tab content
	var lines []string
	switch m.detailTab {
	case 0:
		lines = m.renderMetadataTab(msg)
	case 1:
		lines = m.renderHeadersTab(msg, innerWidth)
	case 2:
		lines = m.renderBodyTab(msg)
	case 3:
		lines = renderDLXTab(msg)
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

	content := tabBar + "\n" + strings.Join(visibleLines, "\n")
	return detailPanelStyle.Width(width).Height(height).Render(content)
}

func (m model) tabCount() int {
	if len(m.messages) > 0 && m.selectedIdx < len(m.messages) && isDLXMessage(m.messages[m.selectedIdx]) {
		return 4
	}
	return 3
}

func (m model) renderDetailTabBar() string {
	tabs := []string{"Metadata", "Headers", "Body"}
	if m.tabCount() == 4 {
		tabs = append(tabs, "Dead Letter")
	}
	var parts []string
	for i, name := range tabs {
		if i == m.detailTab {
			parts = append(parts, selectedMessageStyle.Render(" "+name+" "))
		} else {
			parts = append(parts, mutedStyle.Render(" "+name+" "))
		}
	}
	return strings.Join(parts, mutedStyle.Render("│"))
}

func (m model) renderMetadataTab(msg Message) []string {
	var lines []string
	lines = append(lines, fieldNameStyle.Render("Routing Key: ")+msg.RoutingKey)
	lines = append(lines, fieldNameStyle.Render("Exchange: ")+msg.Exchange)
	lines = append(lines, fieldNameStyle.Render("Timestamp: ")+msg.Timestamp.Format(time.RFC3339))
	lines = append(lines, fieldNameStyle.Render("Size: ")+fmt.Sprintf("%d bytes", len(msg.RawBody)))
	if msg.ContentType != "" {
		lines = append(lines, fieldNameStyle.Render("Content-Type: ")+msg.ContentType)
	}
	if msg.ProtoType != "" {
		lines = append(lines, fieldNameStyle.Render("Proto Type: ")+msg.ProtoType)
	}
	if msg.CorrelationID != "" {
		lines = append(lines, fieldNameStyle.Render("Correlation ID: ")+msg.CorrelationID)
	}
	if msg.MessageID != "" {
		lines = append(lines, fieldNameStyle.Render("Message ID: ")+msg.MessageID)
	}
	if msg.AppID != "" {
		lines = append(lines, fieldNameStyle.Render("App ID: ")+msg.AppID)
	}
	if msg.ReplyTo != "" {
		lines = append(lines, fieldNameStyle.Render("Reply To: ")+msg.ReplyTo)
	}
	return lines
}

func (m model) renderHeadersTab(msg Message, innerWidth int) []string {
	if len(msg.Headers) == 0 {
		return []string{mutedStyle.Render("No headers")}
	}
	headerKeys := make([]string, 0, len(msg.Headers))
	for k := range msg.Headers {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)
	var lines []string
	for _, k := range headerKeys {
		lines = append(lines, fmt.Sprintf("%s: %s", fieldNameStyle.Render(k), formatHeaderValue(msg.Headers[k])))
	}
	return lines
}

func (m model) renderBodyTab(msg Message) []string {
	if m.showRaw {
		return []string{formatHex(msg.RawBody)}
	}
	if msg.DecodeErr != nil {
		return []string{
			errorStyle.Render(fmt.Sprintf("Decode error: %v", msg.DecodeErr)),
			formatHex(msg.RawBody),
		}
	}
	if msg.Decoded != nil {
		return []string{formatJSONSyntax(msg.Decoded)}
	}
	return []string{formatHex(msg.RawBody)}
}

func (m model) renderSearchBar() string {
	return helpStyle.Render("Search: ") + m.searchInput.View() + helpStyle.Render("  (Enter to search, Esc to cancel)")
}

func (m model) renderFilterBar() string {
	return helpStyle.Render("Filter: ") + m.filterInput.View() + helpStyle.Render("  (Enter to apply, Esc to cancel)")
}

func (m model) renderHelpBar() string {
	keys := []struct{ key, desc string }{
		{"j/k", "nav"},
		{"gg/G", "top/end"},
		{"/", "search"},
		{"f/F", "filter"},
		{"y/Y", "copy"},
		{"e/E", "export"},
		{"m", "mark"},
		{"tab", "section"},
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
			name: "Search & Filter",
			keys: []struct{ key, desc string }{
				{"/", "Start search (prefix: rk: body: ex: hdr: type: re:)"},
				{"n / N", "Next / previous result"},
				{"f", "Set filter (same prefixes as search)"},
				{"F", "Toggle filter on/off"},
				{"Esc", "Clear search / cancel filter"},
			},
		},
		{
			name: "Actions",
			keys: []struct{ key, desc string }{
				{"y", "Copy active tab content (routing key / headers / body)"},
				{"Y", "Copy full message to clipboard"},
				{"e", "Export all messages to JSON"},
				{"E", "Export all messages to CSV"},
				{"m", "Toggle bookmark"},
				{"'", "Jump to next bookmark"},
				{"c", "Clear all messages"},
			},
		},
		{
			name: "View",
			keys: []struct{ key, desc string }{
				{"Tab / S-Tab", "Switch detail section"},
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
