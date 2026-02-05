package tui

import (
	"time"
	"unicode"
)

const keyTimeout = 500 * time.Millisecond

// VimKeyState tracks vim-style key sequences and numeric prefixes
type VimKeyState struct {
	pendingKeys   string
	numericPrefix int
	lastKeyTime   time.Time
}

// VimKeyResult represents the result of processing a key
type VimKeyResult struct {
	Action string // The action to perform (e.g., "move_down", "go_top", "search")
	Count  int    // Numeric count (e.g., 5 for "5j")
	Clear  bool   // Whether to clear the pending state
}

// NewVimKeyState creates a new vim key state tracker
func NewVimKeyState() VimKeyState {
	return VimKeyState{}
}

// ProcessKey processes a key press and returns the action to take
func (v *VimKeyState) ProcessKey(key string) VimKeyResult {
	now := time.Now()

	// Reset state if too much time has passed
	if now.Sub(v.lastKeyTime) > keyTimeout {
		v.pendingKeys = ""
		v.numericPrefix = 0
	}
	v.lastKeyTime = now

	// Handle numeric prefix
	if len(key) == 1 {
		r := rune(key[0])
		// Only treat as numeric prefix if it's not the first 0 (which might be a command)
		if unicode.IsDigit(r) && (v.numericPrefix > 0 || r != '0') {
			v.numericPrefix = v.numericPrefix*10 + int(r-'0')
			return VimKeyResult{Action: "pending", Clear: false}
		}
	}

	// Build pending key sequence
	v.pendingKeys += key

	// Check for multi-key sequences
	result := v.matchSequence()
	if result.Action != "" {
		if result.Count == 0 && v.numericPrefix > 0 {
			result.Count = v.numericPrefix
		}
		if result.Count == 0 {
			result.Count = 1
		}
		v.Reset()
		return result
	}

	// Check if this could be the start of a valid sequence
	if v.isPotentialSequence() {
		return VimKeyResult{Action: "pending", Clear: false}
	}

	// Not a valid sequence, reset
	v.Reset()
	return VimKeyResult{Action: "", Clear: true}
}

// matchSequence checks for complete key sequences
func (v *VimKeyState) matchSequence() VimKeyResult {
	switch v.pendingKeys {
	// Navigation
	case "j":
		return VimKeyResult{Action: "move_down", Clear: true}
	case "k":
		return VimKeyResult{Action: "move_up", Clear: true}
	case "gg":
		return VimKeyResult{Action: "go_top", Clear: true}
	case "G":
		return VimKeyResult{Action: "go_bottom", Clear: true}
	case "zz":
		return VimKeyResult{Action: "center_line", Clear: true}

	// Search
	case "/":
		return VimKeyResult{Action: "search_start", Clear: true}
	case "n":
		return VimKeyResult{Action: "search_next", Clear: true}
	case "N":
		return VimKeyResult{Action: "search_prev", Clear: true}

	// Actions
	case "y":
		return VimKeyResult{Action: "yank", Clear: true}
	case "e":
		return VimKeyResult{Action: "export", Clear: true}
	case "m":
		return VimKeyResult{Action: "bookmark_toggle", Clear: true}
	case "'":
		return VimKeyResult{Action: "bookmark_next", Clear: true}

	// View toggles
	case "t":
		return VimKeyResult{Action: "toggle_compact", Clear: true}
	case "T":
		return VimKeyResult{Action: "toggle_timestamp", Clear: true}
	case "r":
		return VimKeyResult{Action: "toggle_raw", Clear: true}
	case "?":
		return VimKeyResult{Action: "toggle_help", Clear: true}

	// Pane resizing
	case "H":
		return VimKeyResult{Action: "resize_left", Clear: true}
	case "L":
		return VimKeyResult{Action: "resize_right", Clear: true}

	// Control
	case "p", " ":
		return VimKeyResult{Action: "pause_toggle", Clear: true}
	case "c":
		return VimKeyResult{Action: "clear", Clear: true}
	case "b":
		return VimKeyResult{Action: "back", Clear: true}
	case "q":
		return VimKeyResult{Action: "quit", Clear: true}
	}

	return VimKeyResult{}
}

// isPotentialSequence checks if current keys could lead to a valid sequence
func (v *VimKeyState) isPotentialSequence() bool {
	potential := []string{"g", "z"}
	for _, p := range potential {
		if v.pendingKeys == p {
			return true
		}
	}
	return false
}

// Reset clears the key state
func (v *VimKeyState) Reset() {
	v.pendingKeys = ""
	v.numericPrefix = 0
}

// GetPending returns the current pending keys for display
func (v *VimKeyState) GetPending() string {
	return v.pendingKeys
}

// GetNumericPrefix returns the current numeric prefix
func (v *VimKeyState) GetNumericPrefix() int {
	return v.numericPrefix
}
