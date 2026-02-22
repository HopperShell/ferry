// internal/ui/theme/theme.go
package theme

import "github.com/charmbracelet/lipgloss"

var (
	// Primary palette - nautical
	Navy    = lipgloss.Color("#1B2838")
	Teal    = lipgloss.Color("#2D9B99")
	Cyan    = lipgloss.Color("#5DE4E7")
	Amber   = lipgloss.Color("#FFAA33")
	Red     = lipgloss.Color("#FF5555")
	Green   = lipgloss.Color("#50FA7B")
	White   = lipgloss.Color("#F8F8F2")
	Dim     = lipgloss.Color("#6272A4")
	BgDark  = lipgloss.Color("#0E1621")
	BgPanel = lipgloss.Color("#1B2838")

	// Component styles
	ActiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Cyan)

	InactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Dim)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Cyan).
			Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
			Background(Navy).
			Foreground(White).
			Padding(0, 1)

	DirStyle  = lipgloss.NewStyle().Bold(true).Foreground(Cyan)
	FileStyle = lipgloss.NewStyle().Foreground(White)
	ExecStyle = lipgloss.NewStyle().Foreground(Green)
	LinkStyle = lipgloss.NewStyle().Foreground(Cyan).Italic(true)
	SizeStyle = lipgloss.NewStyle().Foreground(Dim).Align(lipgloss.Right)

	SelectedStyle = lipgloss.NewStyle().
			Background(Teal).
			Foreground(White)

	CursorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2A3A5A")).
			Foreground(White)

	ProgressBar    = lipgloss.NewStyle().Foreground(Cyan)
	ProgressFilled = lipgloss.NewStyle().Foreground(Amber)

	ErrorStyle   = lipgloss.NewStyle().Foreground(Red).Bold(true)
	WarningStyle = lipgloss.NewStyle().Foreground(Amber)
	SuccessStyle = lipgloss.NewStyle().Foreground(Green)
)
