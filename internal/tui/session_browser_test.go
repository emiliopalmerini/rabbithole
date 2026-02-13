package tui

import (
	"database/sql"
	"testing"
	"time"

	"github.com/epalmerini/rabbithole/internal/db"
)

func makeSessionBrowser(entries []sessionEntry) sessionBrowserModel {
	m := newSessionBrowserModel(Config{}, nil)
	m.sessions = entries
	m.loading = false
	m.width = 120
	m.height = 40
	return m
}

func sampleEntries() []sessionEntry {
	return []sessionEntry{
		{
			session: db.Session{
				ID: 1, Exchange: "orders-exchange", RoutingKey: "#",
				StartedAt: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
				EndedAt:   sql.NullTime{Time: time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC), Valid: true},
			},
			msgCount: 142,
		},
		{
			session: db.Session{
				ID: 2, Exchange: "user-events", RoutingKey: "user.#",
				StartedAt: time.Date(2025, 1, 14, 9, 0, 0, 0, time.UTC),
				EndedAt:   sql.NullTime{Time: time.Date(2025, 1, 14, 10, 0, 0, 0, time.UTC), Valid: true},
			},
			msgCount: 37,
		},
		{
			session: db.Session{
				ID: 3, Exchange: "orders-exchange", RoutingKey: "order.created",
				StartedAt: time.Date(2025, 1, 13, 8, 0, 0, 0, time.UTC),
				EndedAt:   sql.NullTime{Time: time.Date(2025, 1, 13, 9, 0, 0, 0, time.UTC), Valid: true},
			},
			msgCount: 5,
		},
	}
}

func TestSessionBrowserApplyFilter(t *testing.T) {
	t.Run("filter by exchange", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = "orders"
		m.applyFilter()

		if len(m.filteredIdx) != 2 {
			t.Fatalf("expected 2 filtered, got %d", len(m.filteredIdx))
		}
		// Should match entries at index 0 and 2 (both orders-exchange)
		if m.filteredIdx[0] != 0 || m.filteredIdx[1] != 2 {
			t.Errorf("filteredIdx = %v, want [0, 2]", m.filteredIdx)
		}
	})

	t.Run("filter by routing key", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = "user"
		m.applyFilter()

		if len(m.filteredIdx) != 1 {
			t.Fatalf("expected 1 filtered, got %d", len(m.filteredIdx))
		}
		if m.filteredIdx[0] != 1 {
			t.Errorf("filteredIdx = %v, want [1]", m.filteredIdx)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = "nonexistent"
		m.applyFilter()

		if len(m.filteredIdx) != 0 {
			t.Errorf("expected 0 filtered, got %d", len(m.filteredIdx))
		}
	})

	t.Run("empty query clears filter", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = ""
		m.applyFilter()

		if m.filteredIdx != nil {
			t.Errorf("expected nil filteredIdx for empty query, got %v", m.filteredIdx)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = "ORDERS"
		m.applyFilter()

		if len(m.filteredIdx) != 2 {
			t.Errorf("expected 2 case-insensitive matches, got %d", len(m.filteredIdx))
		}
	})
}

func TestSessionBrowserNavigation(t *testing.T) {
	t.Run("index bounds check", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())

		// At index 0, maxIndex should be 2 (3 entries)
		if m.maxIndex() != 2 {
			t.Errorf("maxIndex() = %d, want 2", m.maxIndex())
		}

		// selectedIdx starts at 0
		if m.selectedIdx != 0 {
			t.Errorf("initial selectedIdx = %d, want 0", m.selectedIdx)
		}
	})

	t.Run("getActualIndex without filter", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())

		if idx := m.getActualIndex(1); idx != 1 {
			t.Errorf("getActualIndex(1) = %d, want 1", idx)
		}
	})

	t.Run("getActualIndex with filter", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = "orders"
		m.applyFilter()

		// Display index 0 should map to actual index 0
		if idx := m.getActualIndex(0); idx != 0 {
			t.Errorf("getActualIndex(0) = %d, want 0", idx)
		}
		// Display index 1 should map to actual index 2
		if idx := m.getActualIndex(1); idx != 2 {
			t.Errorf("getActualIndex(1) = %d, want 2", idx)
		}
		// Out-of-bounds display index should return -1
		if idx := m.getActualIndex(5); idx != -1 {
			t.Errorf("getActualIndex(5) = %d, want -1", idx)
		}
	})

	t.Run("displayList without filter", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		list := m.displayList()
		if len(list) != 3 {
			t.Errorf("displayList len = %d, want 3", len(list))
		}
	})

	t.Run("displayList with filter", func(t *testing.T) {
		m := makeSessionBrowser(sampleEntries())
		m.filterQuery = "user"
		m.applyFilter()
		list := m.displayList()
		if len(list) != 1 {
			t.Errorf("displayList len = %d, want 1", len(list))
		}
		if list[0].session.ID != 2 {
			t.Errorf("displayList[0].ID = %d, want 2", list[0].session.ID)
		}
	})

	t.Run("empty sessions", func(t *testing.T) {
		m := makeSessionBrowser(nil)
		if m.maxIndex() != 0 {
			t.Errorf("maxIndex() with no sessions = %d, want 0", m.maxIndex())
		}
	})
}

func TestSessionBrowserFTSFilter(t *testing.T) {
	m := makeSessionBrowser(sampleEntries())

	// Simulate FTS results matching session IDs 1 and 3
	m.ftsMatchIDs = map[int64]bool{1: true, 3: true}
	m.applyFTSFilter()

	if len(m.filteredIdx) != 2 {
		t.Fatalf("expected 2 FTS matches, got %d", len(m.filteredIdx))
	}
	if m.filteredIdx[0] != 0 || m.filteredIdx[1] != 2 {
		t.Errorf("filteredIdx = %v, want [0, 2]", m.filteredIdx)
	}
}

func TestInitialReplayModel(t *testing.T) {
	cfg := Config{
		Exchange:   "test-exchange",
		RoutingKey: "#",
	}
	session := db.Session{
		ID:       1,
		Exchange: "test-exchange",
	}
	dbMsgs := []db.Message{
		{ID: 1, Exchange: "test-exchange", RoutingKey: "order.created", Body: []byte("msg1")},
		{ID: 2, Exchange: "test-exchange", RoutingKey: "order.shipped", Body: []byte("msg2")},
	}

	m := initialReplayModel(cfg, session, dbMsgs)

	if !m.replayMode {
		t.Error("expected replayMode=true")
	}
	if m.connState != stateConnected {
		t.Errorf("connState = %d, want %d (stateConnected)", m.connState, stateConnected)
	}
	if len(m.messages) != 2 {
		t.Errorf("messages len = %d, want 2", len(m.messages))
	}
	if m.messageCount != 2 {
		t.Errorf("messageCount = %d, want 2", m.messageCount)
	}
	if m.store != nil {
		t.Error("expected store=nil in replay mode")
	}
	// All messages should be marked as historical
	for i, msg := range m.messages {
		if !msg.Historical {
			t.Errorf("message %d: expected Historical=true", i)
		}
	}
}
