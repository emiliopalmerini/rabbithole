package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestURLPrompt_SubmitEmitsMsg(t *testing.T) {
	m := newURLPromptModel()

	// Type a URL
	for _, r := range "amqp://test:5672/" {
		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = result.(urlPromptModel)
	}

	// Press enter
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter")
	}
	msg := cmd()
	entered, ok := msg.(urlEnteredMsg)
	if !ok {
		t.Fatalf("expected urlEnteredMsg, got %T", msg)
	}
	if entered.url != "amqp://test:5672/" {
		t.Errorf("url = %q, want %q", entered.url, "amqp://test:5672/")
	}
}

func TestURLPrompt_EmptySubmitUsesPlaceholder(t *testing.T) {
	m := newURLPromptModel()

	// Press enter without typing
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter")
	}
	msg := cmd()
	entered, ok := msg.(urlEnteredMsg)
	if !ok {
		t.Fatalf("expected urlEnteredMsg, got %T", msg)
	}
	if entered.url != defaultAMQPURL {
		t.Errorf("url = %q, want default %q", entered.url, defaultAMQPURL)
	}
}

func TestURLPrompt_QuitEmitsQuit(t *testing.T) {
	m := newURLPromptModel()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}
