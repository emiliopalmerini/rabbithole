package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/epalmerini/rabbithole/internal/db"
)

// sessionEntry holds a session with its message count for display.
type sessionEntry struct {
	session  db.Session
	msgCount int64
}

// Tea messages for session browser
type sessionsLoadedMsg struct {
	entries []sessionEntry
}

type sessionDeletedMsg struct {
	sessionID int64
}

type replaySessionMsg struct {
	session  db.Session
	messages []db.Message
}

type sessionFTSResultMsg struct {
	sessionIDs []int64
}

type sessionBrowserModel struct {
	store  db.Store
	config Config

	width, height int

	sessions    []sessionEntry
	selectedIdx int
	scrollOff   int

	// Metadata filter (/ key)
	searchMode  bool
	searchInput textinput.Model
	filterQuery string
	filteredIdx []int // indices into sessions matching filter

	// FTS content search (S key)
	ftsMode     bool
	ftsInput    textinput.Model
	ftsQuery    string
	ftsMatchIDs map[int64]bool // session IDs matching FTS query

	// Delete confirmation
	confirmDelete bool

	// Spinner / loading
	spinner spinner.Model
	loading bool
	err     error

	// Status message
	statusMsg     string
	statusMsgTime time.Time
}

func newSessionBrowserModel(cfg Config, store db.Store) sessionBrowserModel {
	si := textinput.New()
	si.Placeholder = "Filter by exchange/routing key..."
	si.CharLimit = 100
	si.Width = 40

	ftsInput := textinput.New()
	ftsInput.Placeholder = "Search message content..."
	ftsInput.CharLimit = 100
	ftsInput.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	return sessionBrowserModel{
		store:       store,
		config:      cfg,
		spinner:     sp,
		loading:     true,
		searchInput: si,
		ftsInput:    ftsInput,
	}
}

func (m sessionBrowserModel) Init() tea.Cmd {
	return tea.Batch(
		m.loadSessions(),
		m.spinner.Tick,
	)
}

func (m sessionBrowserModel) loadSessions() tea.Cmd {
	store := m.store
	return func() tea.Msg {
		if store == nil {
			return errorMsg{err: fmt.Errorf("no persistence store")}
		}
		ctx := context.Background()
		sessions, err := store.ListRecentSessions(ctx, 100)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to load sessions: %w", err)}
		}

		entries := make([]sessionEntry, len(sessions))
		for i, s := range sessions {
			count, err := store.CountMessagesBySession(ctx, s.ID)
			if err != nil {
				count = 0
			}
			entries[i] = sessionEntry{session: s, msgCount: count}
		}
		return sessionsLoadedMsg{entries: entries}
	}
}

func (m sessionBrowserModel) loadSessionMessages(session db.Session) tea.Cmd {
	store := m.store
	return func() tea.Msg {
		ctx := context.Background()
		msgs, err := store.ListMessagesBySessionAsc(ctx, session.ID, 10000, 0)
		if err != nil {
			return errorMsg{err: fmt.Errorf("failed to load messages: %w", err)}
		}
		return replaySessionMsg{session: session, messages: msgs}
	}
}

func (m sessionBrowserModel) deleteSession(sessionID int64) tea.Cmd {
	store := m.store
	return func() tea.Msg {
		if err := store.DeleteSession(context.Background(), sessionID); err != nil {
			return errorMsg{err: fmt.Errorf("failed to delete session: %w", err)}
		}
		return sessionDeletedMsg{sessionID: sessionID}
	}
}

func (m sessionBrowserModel) searchFTS(query string) tea.Cmd {
	store := m.store
	return func() tea.Msg {
		ids, err := store.SearchSessionsByContent(context.Background(), query, 100)
		if err != nil {
			return errorMsg{err: fmt.Errorf("FTS search failed: %w", err)}
		}
		return sessionFTSResultMsg{sessionIDs: ids}
	}
}

func (m sessionBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle metadata filter input
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.filterQuery = ""
				m.filteredIdx = nil
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
				m.filterQuery = m.searchInput.Value()
				m.applyFilter()
				return m, cmd
			}
		}

		// Handle FTS content search input
		if m.ftsMode {
			switch msg.String() {
			case "esc":
				m.ftsMode = false
				m.ftsInput.SetValue("")
				m.ftsInput.Blur()
				return m, nil
			case "enter":
				m.ftsMode = false
				m.ftsQuery = m.ftsInput.Value()
				m.ftsInput.Blur()
				if m.ftsQuery != "" {
					m.loading = true
					return m, m.searchFTS(m.ftsQuery)
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.ftsInput, cmd = m.ftsInput.Update(msg)
				return m, cmd
			}
		}

		// Handle delete confirmation
		if m.confirmDelete {
			switch msg.String() {
			case "enter":
				m.confirmDelete = false
				idx := m.getActualIndex(m.selectedIdx)
				if idx >= 0 && idx < len(m.sessions) {
					return m, m.deleteSession(m.sessions[idx].session.ID)
				}
				return m, nil
			case "esc":
				m.confirmDelete = false
				return m, nil
			default:
				return m, nil
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
		case "S":
			m.ftsMode = true
			m.ftsInput.SetValue("")
			m.ftsInput.Focus()
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
				if visibleItems < 1 {
					visibleItems = 1
				}
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
		case "enter":
			idx := m.getActualIndex(m.selectedIdx)
			if idx >= 0 && idx < len(m.sessions) {
				m.loading = true
				return m, m.loadSessionMessages(m.sessions[idx].session)
			}
		case "d":
			idx := m.getActualIndex(m.selectedIdx)
			if idx >= 0 && idx < len(m.sessions) {
				m.confirmDelete = true
			}
		case "esc":
			// Clear FTS filter if active, otherwise clear metadata filter
			if m.ftsQuery != "" {
				m.ftsQuery = ""
				m.ftsMatchIDs = nil
				return m, nil
			}
			if m.filterQuery != "" {
				m.filterQuery = ""
				m.filteredIdx = nil
				m.searchInput.SetValue("")
				m.selectedIdx = 0
				m.scrollOff = 0
				return m, nil
			}
		case "b":
			// Handled by parent
		case "r":
			m.loading = true
			return m, m.loadSessions()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case sessionsLoadedMsg:
		m.loading = false
		m.sessions = msg.entries
		m.applyFilter()
		// Re-apply FTS filter if active
		if m.ftsQuery != "" && m.ftsMatchIDs != nil {
			m.applyFTSFilter()
		}

	case sessionDeletedMsg:
		// Remove deleted session from list
		for i, entry := range m.sessions {
			if entry.session.ID == msg.sessionID {
				m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
				break
			}
		}
		m.applyFilter()
		if m.selectedIdx >= len(m.displayList()) {
			m.selectedIdx = len(m.displayList()) - 1
			if m.selectedIdx < 0 {
				m.selectedIdx = 0
			}
		}
		m.statusMsg = "Session deleted"
		m.statusMsgTime = time.Now()
		cmds = append(cmds, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
			return clearStatusMsg{}
		}))

	case sessionFTSResultMsg:
		m.loading = false
		m.ftsMatchIDs = make(map[int64]bool, len(msg.sessionIDs))
		for _, id := range msg.sessionIDs {
			m.ftsMatchIDs[id] = true
		}
		m.applyFTSFilter()
		m.selectedIdx = 0
		m.scrollOff = 0
		m.statusMsg = fmt.Sprintf("%d sessions match", len(msg.sessionIDs))
		m.statusMsgTime = time.Now()
		cmds = append(cmds, tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
			return clearStatusMsg{}
		}))

	case replaySessionMsg:
		// Handled by parent appModel
		m.loading = false

	case errorMsg:
		m.loading = false
		m.err = msg.err

	case clearStatusMsg:
		m.statusMsg = ""

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *sessionBrowserModel) applyFilter() {
	if m.filterQuery == "" {
		m.filteredIdx = nil
		return
	}

	query := strings.ToLower(m.filterQuery)
	m.filteredIdx = nil
	for i, entry := range m.sessions {
		s := entry.session
		if strings.Contains(strings.ToLower(s.Exchange), query) ||
			strings.Contains(strings.ToLower(s.RoutingKey), query) {
			m.filteredIdx = append(m.filteredIdx, i)
		}
	}
}

func (m *sessionBrowserModel) applyFTSFilter() {
	if m.ftsMatchIDs == nil || len(m.ftsMatchIDs) == 0 {
		// If already filtered by metadata, keep that; otherwise show all
		return
	}

	// Intersect with FTS results: if metadata filter active, filter from that;
	// otherwise filter from full list.
	source := m.filteredIdx
	if source == nil {
		source = make([]int, len(m.sessions))
		for i := range m.sessions {
			source[i] = i
		}
	}

	var result []int
	for _, idx := range source {
		if m.ftsMatchIDs[m.sessions[idx].session.ID] {
			result = append(result, idx)
		}
	}
	m.filteredIdx = result
}

func (m sessionBrowserModel) displayList() []sessionEntry {
	if m.filteredIdx != nil {
		list := make([]sessionEntry, len(m.filteredIdx))
		for i, idx := range m.filteredIdx {
			list[i] = m.sessions[idx]
		}
		return list
	}
	return m.sessions
}

func (m sessionBrowserModel) getActualIndex(displayIdx int) int {
	if m.filteredIdx == nil {
		return displayIdx
	}
	if displayIdx >= 0 && displayIdx < len(m.filteredIdx) {
		return m.filteredIdx[displayIdx]
	}
	return -1
}

func (m sessionBrowserModel) maxIndex() int {
	list := m.displayList()
	if len(list) == 0 {
		return 0
	}
	return len(list) - 1
}

func (m sessionBrowserModel) View() string {
	if m.width == 0 {
		return m.spinner.View() + " Loading..."
	}

	header := headerStyle.Width(m.width - 2).Render(
		"rabbithole - Session Browser",
	)

	content := m.renderSessions()

	var bottomBar string
	if m.searchMode {
		bottomBar = helpStyle.Render("Filter: ") + m.searchInput.View() + helpStyle.Render("  (Enter to apply, Esc to cancel)")
	} else if m.ftsMode {
		bottomBar = helpStyle.Render("Search content: ") + m.ftsInput.View() + helpStyle.Render("  (Enter to search, Esc to cancel)")
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

func (m sessionBrowserModel) renderSessions() string {
	var sb strings.Builder

	title := "Sessions:"
	if m.filterQuery != "" {
		title = fmt.Sprintf("Sessions (filtered: %q):", m.filterQuery)
	}
	if m.ftsQuery != "" {
		title += fmt.Sprintf(" [content: %q]", m.ftsQuery)
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

	// Status message
	if m.statusMsg != "" && time.Since(m.statusMsgTime) < 3*time.Second {
		sb.WriteString("  " + confirmationStyle.Render(m.statusMsg))
		sb.WriteString("\n\n")
	}

	list := m.displayList()
	if len(list) == 0 {
		if m.filterQuery != "" || m.ftsQuery != "" {
			sb.WriteString(mutedStyle.Render("  No sessions match"))
		} else {
			sb.WriteString(mutedStyle.Render("  No sessions found"))
		}
		return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
	}

	visibleItems := m.height - 12
	if visibleItems < 1 {
		visibleItems = 1
	}
	startIdx := m.scrollOff
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + visibleItems
	if endIdx > len(list) {
		endIdx = len(list)
	}

	innerWidth := m.width - 8
	for i := startIdx; i < endIdx; i++ {
		entry := list[i]
		s := entry.session

		// Format time range
		startTime := s.StartedAt.Format("Jan 02 15:04")
		endTime := "running"
		if s.EndedAt.Valid {
			endTime = s.EndedAt.Time.Format("15:04")
		}

		// Delete confirmation indicator
		deleteHint := ""
		if m.confirmDelete && i == m.selectedIdx {
			deleteHint = errorStyle.Render("  [Enter to confirm delete, Esc to cancel]")
		}

		line := fmt.Sprintf("%-20s  %s  │  %d msgs  │  %s → %s",
			truncate(s.Exchange, 20),
			routingKeyStyle.Render(truncate(s.RoutingKey, 15)),
			entry.msgCount,
			startTime,
			endTime,
		)

		if i == m.selectedIdx {
			sb.WriteString(selectedMessageStyle.Width(innerWidth).Render("▶ " + line + deleteHint))
		} else {
			sb.WriteString(normalMessageStyle.Width(innerWidth).Render("  " + line))
		}
		sb.WriteString("\n")
	}

	return messageListStyle.Width(m.width - 4).Height(m.height - 8).Render(sb.String())
}

func (m sessionBrowserModel) renderHelp() string {
	keys := []struct{ key, desc string }{
		{"j/k", "navigate"},
		{"/", "filter"},
		{"S", "search content"},
		{"enter", "replay"},
		{"d", "delete"},
		{"r", "refresh"},
		{"b", "back"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %s", helpKeyStyle.Render(k.key), k.desc))
	}

	return helpStyle.Render(strings.Join(parts, "  "))
}
