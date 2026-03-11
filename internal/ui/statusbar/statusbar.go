// internal/ui/statusbar/statusbar.go
package statusbar

import (
	"fmt"
	"strings"

	"github.com/HopperShell/ferry/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

var (
	keyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.Navy).
			Background(theme.Cyan)

	labelStyle = lipgloss.NewStyle().
			Foreground(theme.White).
			Background(theme.Navy)
)

// hint is a single keybinding hint for the bottom bar.
type hint struct {
	key   string
	label string
}

// StatusBar is a simple renderer for the bottom status line.
// It is NOT a tea.Model — just a view component.
type StatusBar struct {
	connection string
	selection  int
	error      string
	width      int
}

// SetConnection sets the connection info string (e.g., "user@host:22").
func (s *StatusBar) SetConnection(info string) {
	s.connection = info
}

// SetSelection sets the selected file count.
func (s *StatusBar) SetSelection(count int) {
	s.selection = count
}

// SetError sets an error message to display. Pass "" to clear.
func (s *StatusBar) SetError(msg string) {
	s.error = msg
}

// HasError returns whether the status bar is currently showing an error.
func (s *StatusBar) HasError() bool {
	return s.error != ""
}

// SetWidth sets the available width for the status bar.
func (s *StatusBar) SetWidth(width int) {
	s.width = width
}

// Height returns the number of rows the status bar occupies.
func (s *StatusBar) Height() int {
	return 2
}

// View renders the status bar as two lines: info line + keybinding hints.
func (s *StatusBar) View() string {
	width := s.width
	if width < 20 {
		width = 20
	}

	infoLine := s.renderInfoLine(width)
	hintLine := s.renderHintLine(width)

	return lipgloss.JoinVertical(lipgloss.Left, infoLine, hintLine)
}

func (s *StatusBar) renderInfoLine(width int) string {
	// If there's an error/message, show it across the full bar.
	if s.error != "" {
		errText := theme.ErrorStyle.Render(" " + s.error + " ")
		pad := width - lipgloss.Width(errText)
		if pad < 0 {
			pad = 0
		}
		line := errText + strings.Repeat(" ", pad)
		return theme.StatusBar.Width(width).Render(line)
	}

	// Left: connection info.
	left := s.connection
	if left == "" {
		left = "not connected"
	}
	leftStyled := lipgloss.NewStyle().Foreground(theme.Cyan).Render(left)

	// Right: selection count.
	var right string
	if s.selection > 0 {
		right = fmt.Sprintf("%d files selected", s.selection)
	}
	rightStyled := lipgloss.NewStyle().Foreground(theme.Amber).Render(right)

	gap := width - lipgloss.Width(leftStyled) - lipgloss.Width(rightStyled) - 2
	if gap < 1 {
		gap = 1
	}

	line := leftStyled + strings.Repeat(" ", gap) + rightStyled
	return theme.StatusBar.Width(width).Render(line)
}

func (s *StatusBar) renderHintLine(width int) string {
	hints := []hint{
		{"Enter", "Transfer"},
		{"Tab", "Switch"},
		{"Space", "Select"},
		{"yy", "Copy"},
		{"p", "Paste"},
		{"dd", "Delete"},
		{"r", "Rename"},
		{"D", "Mkdir"},
		{"e", "Edit"},
		{"S", "Sync"},
		{"?", "Help"},
		{"q", "Quit"},
	}

	var parts []string
	for _, h := range hints {
		parts = append(parts, keyStyle.Render(" "+h.key+" ")+labelStyle.Render(h.label))
	}

	line := strings.Join(parts, " ")

	// Pad to fill the full width.
	lineW := lipgloss.Width(line)
	if lineW < width {
		line += labelStyle.Render(strings.Repeat(" ", width-lineW))
	}

	return line
}
