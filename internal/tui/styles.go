package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Color palette - minimalist theme
	orangeColor   = lipgloss.Color("#FF9500") // Headers, accents, selected items, field names
	offWhiteColor = lipgloss.Color("#E8E8E8") // Primary text
	greyColor     = lipgloss.Color("#6B7280") // Borders, muted text, secondary elements
	darkBgColor   = lipgloss.Color("#1A1A1A") // Background
	greenColor    = lipgloss.Color("#22C55E") // Connected status
	redColor      = lipgloss.Color("#EF4444") // Errors, disconnected

	// JSON syntax highlighting
	jsonKeyColor    = lipgloss.Color("#FF9500") // Orange bold
	jsonStringColor = lipgloss.Color("#22C55E") // Green
	jsonNumberColor = lipgloss.Color("#60A5FA") // Blue
	jsonBoolColor   = lipgloss.Color("#F472B6") // Pink
	jsonNullColor   = lipgloss.Color("#6B7280") // Grey italic

	// Header
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(orangeColor).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(greyColor).
			Padding(0, 1).
			MarginBottom(1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(greyColor).
			Padding(0, 1)

	connectedStyle = lipgloss.NewStyle().
			Foreground(greenColor).
			Bold(true)

	disconnectedStyle = lipgloss.NewStyle().
				Foreground(redColor).
				Bold(true)

	// Message list
	messageListStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(greyColor).
				Padding(0, 1)

	selectedMessageStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2D2D2D")).
				Foreground(orangeColor).
				Bold(true)

	normalMessageStyle = lipgloss.NewStyle().
				Foreground(offWhiteColor)

	// Routing key styles
	routingKeyStyle = lipgloss.NewStyle().
			Foreground(orangeColor).
			Italic(true)

	// Detail panel
	detailPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(greyColor).
				Padding(1)

	fieldNameStyle = lipgloss.NewStyle().
			Foreground(orangeColor).
			Bold(true)

	// Help bar
	helpStyle = lipgloss.NewStyle().
			Foreground(greyColor).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(orangeColor).
			Bold(true)

	// Utility styles
	mutedStyle = lipgloss.NewStyle().
			Foreground(greyColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(redColor)

	// Section divider
	dividerStyle = lipgloss.NewStyle().
			Foreground(greyColor)

	// JSON syntax highlighting styles
	jsonKeyStyle = lipgloss.NewStyle().
			Foreground(jsonKeyColor).
			Bold(true)

	jsonStringStyle = lipgloss.NewStyle().
			Foreground(jsonStringColor)

	jsonNumberStyle = lipgloss.NewStyle().
			Foreground(jsonNumberColor)

	jsonBoolStyle = lipgloss.NewStyle().
			Foreground(jsonBoolColor)

	jsonNullStyle = lipgloss.NewStyle().
			Foreground(jsonNullColor).
			Italic(true)

	// Bookmark indicator
	bookmarkStyle = lipgloss.NewStyle().
			Foreground(orangeColor).
			Bold(true)

	// Help overlay
	helpOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(orangeColor).
				Padding(1, 2).
				Background(darkBgColor)

	helpCategoryStyle = lipgloss.NewStyle().
				Foreground(orangeColor).
				Bold(true).
				MarginTop(1)

	// Spinner style
	spinnerStyle = lipgloss.NewStyle().
			Foreground(orangeColor)

	// New message indicator
	newMsgStyle = lipgloss.NewStyle().
			Foreground(greenColor).
			Bold(true)

	// Empty state
	emptyStateStyle = lipgloss.NewStyle().
			Foreground(greyColor).
			Italic(true).
			Align(lipgloss.Center)

	// Confirmation message (for clipboard copy, export, etc.)
	confirmationStyle = lipgloss.NewStyle().
				Foreground(greenColor)

	// Dead-letter indicator
	dlxStyle = lipgloss.NewStyle().
			Foreground(redColor)
)
