package tui

import (
	"testing"
	"time"
)

func TestProcessKey_SingleKeys(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantAction string
		wantCount  int
	}{
		{"j moves down", "j", "move_down", 1},
		{"k moves up", "k", "move_up", 1},
		{"G goes to bottom", "G", "go_bottom", 1},
		{"/ starts search", "/", "search_start", 1},
		{"n next search", "n", "search_next", 1},
		{"N prev search", "N", "search_prev", 1},
		{"q quits", "q", "quit", 1},
		{"y yanks routing key", "y", "yank_routing_key", 1},
		{"Y yanks full message", "Y", "yank", 1},
		{"e exports", "e", "export", 1},
		{"r toggles raw", "r", "toggle_raw", 1},
		{"t toggles compact", "t", "toggle_compact", 1},
		{"T toggles timestamp", "T", "toggle_timestamp", 1},
		{"? toggles help", "?", "toggle_help", 1},
		{"p pauses", "p", "pause_toggle", 1},
		{"space pauses", " ", "pause_toggle", 1},
		{"c clears", "c", "clear", 1},
		{"b goes back", "b", "back", 1},
		{"H resizes left", "H", "resize_left", 1},
		{"L resizes right", "L", "resize_right", 1},
		{"m toggles bookmark", "m", "bookmark_toggle", 1},
		{"' next bookmark", "'", "bookmark_next", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewVimKeyState()
			result := v.ProcessKey(tt.key)
			if result.Action != tt.wantAction {
				t.Errorf("ProcessKey(%q).Action = %q, want %q", tt.key, result.Action, tt.wantAction)
			}
			if result.Count != tt.wantCount {
				t.Errorf("ProcessKey(%q).Count = %d, want %d", tt.key, result.Count, tt.wantCount)
			}
		})
	}
}

func TestProcessKey_MultiKeySequences(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantAction string
	}{
		{"gg goes to top", []string{"g", "g"}, "go_top"},
		{"zz centers line", []string{"z", "z"}, "center_line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewVimKeyState()
			var result VimKeyResult
			for _, k := range tt.keys {
				result = v.ProcessKey(k)
			}
			if result.Action != tt.wantAction {
				t.Errorf("keys %v → Action = %q, want %q", tt.keys, result.Action, tt.wantAction)
			}
		})
	}
}

func TestProcessKey_NumericPrefix(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantAction string
		wantCount  int
	}{
		{"5j moves down 5", []string{"5", "j"}, "move_down", 5},
		{"10k moves up 10", []string{"1", "0", "k"}, "move_up", 10},
		{"3G goes to bottom with count", []string{"3", "G"}, "go_bottom", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewVimKeyState()
			var result VimKeyResult
			for _, k := range tt.keys {
				result = v.ProcessKey(k)
			}
			if result.Action != tt.wantAction {
				t.Errorf("keys %v → Action = %q, want %q", tt.keys, result.Action, tt.wantAction)
			}
			if result.Count != tt.wantCount {
				t.Errorf("keys %v → Count = %d, want %d", tt.keys, result.Count, tt.wantCount)
			}
		})
	}
}

func TestProcessKey_PendingState(t *testing.T) {
	v := NewVimKeyState()

	// "g" alone should be pending
	result := v.ProcessKey("g")
	if result.Action != "pending" {
		t.Errorf("single 'g' → Action = %q, want %q", result.Action, "pending")
	}

	// "z" alone should be pending
	v = NewVimKeyState()
	result = v.ProcessKey("z")
	if result.Action != "pending" {
		t.Errorf("single 'z' → Action = %q, want %q", result.Action, "pending")
	}
}

func TestProcessKey_InvalidSequenceResets(t *testing.T) {
	v := NewVimKeyState()

	// "g" then "x" is not a valid sequence, should reset
	result := v.ProcessKey("g")
	if result.Action != "pending" {
		t.Fatalf("expected pending after 'g', got %q", result.Action)
	}

	result = v.ProcessKey("x")
	if result.Action != "" {
		t.Errorf("'g' then 'x' → Action = %q, want empty (reset)", result.Action)
	}
	if !result.Clear {
		t.Error("expected Clear=true on invalid sequence")
	}

	// After reset, normal keys should work
	result = v.ProcessKey("j")
	if result.Action != "move_down" {
		t.Errorf("after reset, 'j' → Action = %q, want %q", result.Action, "move_down")
	}
}

func TestProcessKey_TimeoutResetsState(t *testing.T) {
	v := NewVimKeyState()

	// Press "g" and set lastKeyTime in the past
	v.ProcessKey("g")
	v.lastKeyTime = time.Now().Add(-2 * keyTimeout)

	// Next key after timeout should not form "gg"
	result := v.ProcessKey("g")
	// After timeout, state resets, so this is a fresh "g" → pending
	if result.Action != "pending" {
		t.Errorf("after timeout, 'g' → Action = %q, want %q", result.Action, "pending")
	}
}
