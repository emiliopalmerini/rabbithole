package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/epalmerini/rabbithole/internal/config"
)

type profileSelectedMsg struct {
	name string
}

type profilePickerModel struct {
	profiles    map[string]config.Profile
	names       []string
	selectedIdx int
	width       int
	height      int
}

func newProfilePickerModel(profiles map[string]config.Profile) profilePickerModel {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	return profilePickerModel{
		profiles: profiles,
		names:    names,
	}
}

func (m profilePickerModel) Init() tea.Cmd {
	return nil
}

func (m profilePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.selectedIdx < len(m.names)-1 {
				m.selectedIdx++
			}
		case "k", "up":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
		case "enter":
			if len(m.names) > 0 {
				name := m.names[m.selectedIdx]
				return m, func() tea.Msg {
					return profileSelectedMsg{name: name}
				}
			}
		}
	}
	return m, nil
}

func (m profilePickerModel) View() string {
	var sb strings.Builder

	header := headerStyle.Width(m.width - 2).Render("rabbithole")
	sb.WriteString(header)
	sb.WriteString("\n\n")

	sb.WriteString(fieldNameStyle.Render("  Select a connection profile"))
	sb.WriteString("\n\n")

	for i, name := range m.names {
		profile := m.profiles[name]
		cursor := "  "
		if i == m.selectedIdx {
			cursor = "> "
		}

		line := fmt.Sprintf("%s%s", cursor, name)
		url := mutedStyle.Render(fmt.Sprintf("  %s", profile.URL))

		if i == m.selectedIdx {
			line = selectedMessageStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString(url)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			helpKeyStyle.Render("j/k")+" navigate",
			"  │  ",
			helpKeyStyle.Render("enter")+" select",
			"  │  ",
			helpKeyStyle.Render("q")+" quit",
		),
	))

	return sb.String()
}
