// internal/ui/modal/help.go
package modal

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/HopperShell/ferry/internal/ui/theme"
)

// HelpOverlay renders a centered keybinding reference modal.
type HelpOverlay struct {
	visible bool
	width   int
	height  int
}

// NewHelpOverlay creates a new HelpOverlay.
func NewHelpOverlay() *HelpOverlay {
	return &HelpOverlay{}
}

// SetVisible sets overlay visibility.
func (h *HelpOverlay) SetVisible(v bool) {
	h.visible = v
}

// Toggle flips overlay visibility.
func (h *HelpOverlay) Toggle() {
	h.visible = !h.visible
}

// IsVisible returns whether the overlay is currently shown.
func (h *HelpOverlay) IsVisible() bool {
	return h.visible
}

// SetSize updates the available terminal size for centering.
func (h *HelpOverlay) SetSize(w, ht int) {
	h.width = w
	h.height = ht
}

// View renders the help overlay as a centered modal.
func (h *HelpOverlay) View() string {
	if !h.visible {
		return ""
	}

	keyStyle := lipgloss.NewStyle().Foreground(theme.Cyan).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(theme.White)
	headerStyle := lipgloss.NewStyle().Foreground(theme.Amber).Bold(true).Underline(true)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Cyan)
	footerStyle := lipgloss.NewStyle().Foreground(theme.Dim).Italic(true)

	// Helper to format a key-description pair with fixed widths.
	entry := func(key, desc string) string {
		return keyStyle.Render(padRight(key, 10)) + descStyle.Render(desc)
	}

	// Build left column: Navigation.
	leftLines := []string{
		headerStyle.Render("Navigation"),
		entry("j/\u2193", "Move down"),
		entry("k/\u2191", "Move up"),
		entry("h/\u2190/Bksp", "Parent dir"),
		entry("l/\u2192/Enter", "Open dir"),
		entry("gg", "Top"),
		entry("G", "Bottom"),
		entry("Ctrl+d", "Page down"),
		entry("Ctrl+u", "Page up"),
		entry("Tab", "Switch pane"),
		entry("/", "Search"),
		entry("Ctrl+f", "Find (recursive)"),
		entry("H", "Hidden files"),
		entry("s", "Cycle sort"),
		entry("Space", "Select"),
		entry("V", "Range select"),
	}

	// Build right column: File Operations + Views.
	rightLines := []string{
		headerStyle.Render("File Operations"),
		entry("yy", "Copy"),
		entry("p", "Paste"),
		entry("dd", "Delete"),
		entry("r", "Rename"),
		entry("m", "Move"),
		entry("D", "Create dir"),
		entry("e", "Edit file"),
		"",
		headerStyle.Render("Views"),
		entry("i", "File info"),
		entry("t", "Transfers"),
		entry("?", "This help"),
		entry("Esc", "Close panel"),
		entry("q", "Quit"),
		"",
		headerStyle.Render("Sync View"),
		entry("M+→/←", "Mirror push/pull"),
		"",
		headerStyle.Render("Mouse"),
		entry("Click", "Select / focus"),
		entry("Dbl-click", "Open / transfer"),
		entry("Right-click", "Context menu"),
		entry("Scroll", "Navigate list"),
		entry("Drag", "Range select"),
	}

	// Pad columns to same height.
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	// Join columns side by side.
	colGap := "    "
	var rows []string
	for i := range leftLines {
		left := padRight(leftLines[i], 30)
		right := rightLines[i]
		rows = append(rows, left+colGap+right)
	}

	content := strings.Join(rows, "\n")
	footer := "\n" + footerStyle.Render("Press Esc to close")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Padding(1, 2).
		Render(
			titleStyle.Render("Keybindings") + "\n\n" + content + footer,
		)

	// Center the box in the terminal.
	if h.width > 0 && h.height > 0 {
		return lipgloss.Place(h.width, h.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}

// padRight pads a string with spaces to at least the given width.
// It accounts for ANSI escape codes by using lipgloss.Width.
func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
