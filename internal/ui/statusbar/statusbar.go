// internal/ui/statusbar/statusbar.go
package statusbar

import (
	"fmt"
	"strings"

	"github.com/andrewstuart/ferry/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

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

// SetWidth sets the available width for the status bar.
func (s *StatusBar) SetWidth(width int) {
	s.width = width
}

// View renders the status bar as a single line.
func (s *StatusBar) View() string {
	width := s.width
	if width < 20 {
		width = 20
	}

	// If there's an error, show it in red across the full bar.
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

	// Middle: selection count.
	var middle string
	if s.selection > 0 {
		middle = fmt.Sprintf("%d files selected", s.selection)
	}
	middleStyled := lipgloss.NewStyle().Foreground(theme.Amber).Render(middle)

	// Right: keybinding hints.
	right := "Tab:switch  ?:help  q:quit"
	rightStyled := lipgloss.NewStyle().Foreground(theme.Dim).Render(right)

	// Calculate spacing.
	leftW := lipgloss.Width(leftStyled)
	midW := lipgloss.Width(middleStyled)
	rightW := lipgloss.Width(rightStyled)
	usedWidth := leftW + midW + rightW

	// Distribute remaining space as gaps.
	remaining := width - usedWidth - 2 // -2 for padding
	if remaining < 2 {
		remaining = 2
	}
	gap1 := remaining / 2
	gap2 := remaining - gap1

	line := leftStyled + strings.Repeat(" ", gap1) + middleStyled + strings.Repeat(" ", gap2) + rightStyled

	return theme.StatusBar.Width(width).Render(line)
}
