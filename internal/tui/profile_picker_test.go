package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/config"
)

func TestProfilePicker_SelectEmitsMsg(t *testing.T) {
	profiles := map[string]config.Profile{
		"local":   {URL: "amqp://localhost:5672/"},
		"staging": {URL: "amqp://staging:5672/"},
	}
	m := newProfilePickerModel(profiles)

	// Press enter on first item
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	picker := result.(profilePickerModel)
	_ = picker

	if cmd == nil {
		t.Fatal("expected a command from enter key")
	}
	msg := cmd()
	sel, ok := msg.(profileSelectedMsg)
	if !ok {
		t.Fatalf("expected profileSelectedMsg, got %T", msg)
	}
	// First alphabetically is "local"
	if sel.name != "local" {
		t.Errorf("selected = %q, want %q", sel.name, "local")
	}
}

func TestProfilePicker_Navigation(t *testing.T) {
	profiles := map[string]config.Profile{
		"aaa": {URL: "amqp://aaa:5672/"},
		"bbb": {URL: "amqp://bbb:5672/"},
		"ccc": {URL: "amqp://ccc:5672/"},
	}
	m := newProfilePickerModel(profiles)

	if m.selectedIdx != 0 {
		t.Fatalf("initial selectedIdx = %d, want 0", m.selectedIdx)
	}

	// Move down with j
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(profilePickerModel)
	if m.selectedIdx != 1 {
		t.Errorf("after j: selectedIdx = %d, want 1", m.selectedIdx)
	}

	// Move down with j again
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(profilePickerModel)
	if m.selectedIdx != 2 {
		t.Errorf("after j j: selectedIdx = %d, want 2", m.selectedIdx)
	}

	// Move down past end should clamp
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(profilePickerModel)
	if m.selectedIdx != 2 {
		t.Errorf("after j past end: selectedIdx = %d, want 2", m.selectedIdx)
	}

	// Move up with k
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(profilePickerModel)
	if m.selectedIdx != 1 {
		t.Errorf("after k: selectedIdx = %d, want 1", m.selectedIdx)
	}
}

func TestProfilePicker_QuitEmitsQuit(t *testing.T) {
	profiles := map[string]config.Profile{
		"local": {URL: "amqp://localhost:5672/"},
	}
	m := newProfilePickerModel(profiles)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}
