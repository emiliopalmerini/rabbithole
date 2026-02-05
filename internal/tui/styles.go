package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#FF6B6B")
	secondaryColor = lipgloss.Color("#4ECDC4")
	accentColor    = lipgloss.Color("#FFE66D")
	mutedColor     = lipgloss.Color("#6C757D")
	successColor   = lipgloss.Color("#2ECC71")
	errorColor     = lipgloss.Color("#E74C3C")
	bgColor        = lipgloss.Color("#1A1A2E")
	fgColor        = lipgloss.Color("#EAEAEA")

	// Base styles
	baseStyle = lipgloss.NewStyle().
			Background(bgColor).
			Foreground(fgColor)

	// Header
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1).
			MarginBottom(1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	connectedStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	disconnectedStyle = lipgloss.NewStyle().
				Foreground(errorColor).
				Bold(true)

	// Message list
	messageListStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(secondaryColor).
				Padding(0, 1)

	selectedMessageStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2D2D44")).
				Foreground(accentColor).
				Bold(true)

	normalMessageStyle = lipgloss.NewStyle().
				Foreground(fgColor)

	// Routing key styles
	routingKeyStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Italic(true)

	timestampStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	// Detail panel
	detailPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(1)

	fieldNameStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

	fieldValueStyle = lipgloss.NewStyle().
			Foreground(fgColor)

	// Help bar
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	// Utility styles
	mutedStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor)
)
