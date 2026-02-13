package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const defaultAMQPURL = "amqp://guest:guest@localhost:5672/"

type urlEnteredMsg struct {
	url string
}

type urlPromptModel struct {
	input         textinput.Model
	width, height int
}

func newURLPromptModel() urlPromptModel {
	ti := textinput.New()
	ti.Placeholder = defaultAMQPURL
	ti.CharLimit = 200
	ti.Width = 60
	ti.Focus()

	return urlPromptModel{
		input: ti,
	}
}

func (m urlPromptModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m urlPromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			url := m.input.Value()
			if url == "" {
				url = defaultAMQPURL
			}
			return m, func() tea.Msg {
				return urlEnteredMsg{url: url}
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m urlPromptModel) View() string {
	var sb strings.Builder

	header := headerStyle.Width(m.width - 2).Render("rabbithole")
	sb.WriteString(header)
	sb.WriteString("\n\n")

	sb.WriteString(fieldNameStyle.Render("  Connect to RabbitMQ"))
	sb.WriteString("\n\n")

	sb.WriteString("  ")
	sb.WriteString(mutedStyle.Render("URL: "))
	sb.WriteString(m.input.View())
	sb.WriteString("\n\n")

	sb.WriteString(helpStyle.Render("  Press enter to connect, ctrl+c to quit"))

	return sb.String()
}
