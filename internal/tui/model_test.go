package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{
			name: "short string fits",
			s:    "hello",
			max:  10,
			want: "hello",
		},
		{
			name: "exact fit",
			s:    "hello",
			max:  5,
			want: "hello",
		},
		{
			name: "long string trimmed",
			s:    "hello world",
			max:  8,
			want: "hello...",
		},
		{
			name: "max <= 3 returns unchanged",
			s:    "hello",
			max:  3,
			want: "hello",
		},
		{
			name: "max = 2 returns unchanged",
			s:    "hi there",
			max:  2,
			want: "hi there",
		},
		{
			name: "empty string",
			s:    "",
			max:  10,
			want: "",
		},
		{
			name: "unicode rune boundary",
			s:    "abcdef",
			max:  6,
			want: "abcdef",
		},
		{
			name: "truncate with max=4",
			s:    "abcdef",
			max:  4,
			want: "a...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestFormatRelativeTime(t *testing.T) {
	tests := []struct {
		name   string
		offset time.Duration
		want   string
	}{
		{"now", 0, "now"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"5 minutes", 5 * time.Minute, "5m"},
		{"3 hours", 3 * time.Hour, "3h"},
		{"2 days", 48 * time.Hour, "2d"},
		{"half second is now", 500 * time.Millisecond, "now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := time.Now().Add(-tt.offset)
			got := formatRelativeTime(ts)
			if got != tt.want {
				t.Errorf("formatRelativeTime(now - %v) = %q, want %q", tt.offset, got, tt.want)
			}
		})
	}
}

func TestMoveBy(t *testing.T) {
	makeModel := func(numMsgs int, selectedIdx int) model {
		msgs := make([]Message, numMsgs)
		for i := range msgs {
			msgs[i] = Message{ID: i + 1, RoutingKey: "test"}
		}
		return model{
			messages:       msgs,
			selectedIdx:    selectedIdx,
			detailViewport: viewport.New(80, 20),
		}
	}

	t.Run("move within bounds", func(t *testing.T) {
		m := makeModel(10, 5)
		m.moveBy(2)
		if m.selectedIdx != 7 {
			t.Errorf("selectedIdx = %d, want 7", m.selectedIdx)
		}
	})

	t.Run("clamp at zero", func(t *testing.T) {
		m := makeModel(10, 2)
		m.moveBy(-5)
		if m.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", m.selectedIdx)
		}
	})

	t.Run("clamp at end", func(t *testing.T) {
		m := makeModel(10, 8)
		m.moveBy(5)
		if m.selectedIdx != 9 {
			t.Errorf("selectedIdx = %d, want 9", m.selectedIdx)
		}
	})

	t.Run("detail viewport resets on selection change", func(t *testing.T) {
		m := makeModel(10, 5)
		m.detailViewport.YOffset = 10
		m.moveBy(1)
		if m.detailViewport.YOffset != 0 {
			t.Errorf("detailViewport.YOffset = %d, want 0", m.detailViewport.YOffset)
		}
	})

	t.Run("auto-pause when configured", func(t *testing.T) {
		m := makeModel(10, 5)
		m.config.AutoPauseOnSelect = true
		m.moveBy(1)
		if !m.paused {
			t.Error("expected paused=true with AutoPauseOnSelect")
		}
	})

	t.Run("empty messages stays at zero", func(t *testing.T) {
		m := makeModel(0, 0)
		m.moveBy(1)
		if m.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", m.selectedIdx)
		}
	})
}

func TestPerformSearch(t *testing.T) {
	makeModel := func() model {
		return model{
			messages: []Message{
				{ID: 1, RoutingKey: "order.created", Decoded: map[string]any{"id": 1, "status": "new"}},
				{ID: 2, RoutingKey: "user.updated", Decoded: map[string]any{"name": "alice"}},
				{ID: 3, RoutingKey: "order.shipped", Decoded: map[string]any{"id": 2, "status": "shipped"}},
			},
			detailViewport: viewport.New(80, 20),
		}
	}

	t.Run("match in routing key", func(t *testing.T) {
		m := makeModel()
		m.searchQuery = "order"
		m.performSearch()
		if len(m.searchResults) != 2 {
			t.Fatalf("expected 2 results, got %d", len(m.searchResults))
		}
		if m.searchResults[0] != 0 || m.searchResults[1] != 2 {
			t.Errorf("searchResults = %v, want [0, 2]", m.searchResults)
		}
		// Should jump to first result
		if m.selectedIdx != 0 {
			t.Errorf("selectedIdx = %d, want 0", m.selectedIdx)
		}
	})

	t.Run("match in decoded body", func(t *testing.T) {
		m := makeModel()
		m.searchQuery = "alice"
		m.performSearch()
		if len(m.searchResults) != 1 {
			t.Fatalf("expected 1 result, got %d", len(m.searchResults))
		}
		if m.searchResults[0] != 1 {
			t.Errorf("searchResults = %v, want [1]", m.searchResults)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		m := makeModel()
		m.searchQuery = "nonexistent"
		m.performSearch()
		if len(m.searchResults) != 0 {
			t.Errorf("expected 0 results, got %d", len(m.searchResults))
		}
	})

	t.Run("empty query is no-op", func(t *testing.T) {
		m := makeModel()
		m.searchQuery = ""
		m.performSearch()
		if m.searchResults != nil {
			t.Errorf("expected nil searchResults for empty query, got %v", m.searchResults)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		m := makeModel()
		m.searchQuery = "ORDER"
		m.performSearch()
		if len(m.searchResults) != 2 {
			t.Errorf("expected 2 results for case-insensitive search, got %d", len(m.searchResults))
		}
	})

	t.Run("field prefix rk: searches routing key only", func(t *testing.T) {
		m := makeModel()
		// "order" appears in routing keys and body; rk: should only match routing keys
		m.searchQuery = "rk:order"
		m.performSearch()
		if len(m.searchResults) != 2 {
			t.Fatalf("expected 2 results, got %d", len(m.searchResults))
		}
		// Should NOT match body content
		m.searchQuery = "rk:alice"
		m.performSearch()
		if len(m.searchResults) != 0 {
			t.Errorf("expected 0 results for rk:alice, got %d", len(m.searchResults))
		}
	})

	t.Run("field prefix body: searches body only", func(t *testing.T) {
		m := makeModel()
		m.searchQuery = "body:alice"
		m.performSearch()
		if len(m.searchResults) != 1 {
			t.Fatalf("expected 1 result, got %d", len(m.searchResults))
		}
		if m.searchResults[0] != 1 {
			t.Errorf("searchResults = %v, want [1]", m.searchResults)
		}
		// "order" appears in routing keys but also in body ("status":"new"/"shipped")
		m.searchQuery = "body:order"
		m.performSearch()
		if len(m.searchResults) != 0 {
			t.Errorf("expected 0 results for body:order (not in body), got %d", len(m.searchResults))
		}
	})

	t.Run("field prefix ex: searches exchange only", func(t *testing.T) {
		m := model{
			messages: []Message{
				{ID: 1, Exchange: "events", RoutingKey: "order.created"},
				{ID: 2, Exchange: "commands", RoutingKey: "user.updated"},
			},
			detailViewport: viewport.New(80, 20),
		}
		m.searchQuery = "ex:events"
		m.performSearch()
		if len(m.searchResults) != 1 {
			t.Fatalf("expected 1 result, got %d", len(m.searchResults))
		}
		if m.searchResults[0] != 0 {
			t.Errorf("searchResults = %v, want [0]", m.searchResults)
		}
	})

	t.Run("field prefix hdr: searches headers", func(t *testing.T) {
		m := model{
			messages: []Message{
				{ID: 1, RoutingKey: "a", Headers: map[string]any{"x-trace-id": "abc123"}},
				{ID: 2, RoutingKey: "b", Headers: map[string]any{"x-source": "web"}},
			},
			detailViewport: viewport.New(80, 20),
		}
		m.searchQuery = "hdr:abc123"
		m.performSearch()
		if len(m.searchResults) != 1 {
			t.Fatalf("expected 1 result, got %d", len(m.searchResults))
		}
		if m.searchResults[0] != 0 {
			t.Errorf("searchResults = %v, want [0]", m.searchResults)
		}
	})

	t.Run("field prefix type: searches proto type", func(t *testing.T) {
		m := model{
			messages: []Message{
				{ID: 1, RoutingKey: "a", ProtoType: "CountryUpdated"},
				{ID: 2, RoutingKey: "b", ProtoType: "UserCreated"},
			},
			detailViewport: viewport.New(80, 20),
		}
		m.searchQuery = "type:country"
		m.performSearch()
		if len(m.searchResults) != 1 {
			t.Fatalf("expected 1 result, got %d", len(m.searchResults))
		}
		if m.searchResults[0] != 0 {
			t.Errorf("searchResults = %v, want [0]", m.searchResults)
		}
	})
}
