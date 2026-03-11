// internal/ui/contextmenu/menu.go
package contextmenu

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/HopperShell/ferry/internal/ui/theme"
)

// Item represents a single menu entry.
type Item struct {
	Label  string
	Action string // identifier returned when selected
}

// SelectMsg is emitted when a menu item is selected.
type SelectMsg struct {
	Action string
}

// DismissMsg is emitted when the menu is dismissed without selection.
type DismissMsg struct{}

// Model is the context menu state.
type Model struct {
	items   []Item
	cursor  int
	x, y    int // terminal position of top-left corner
	visible bool
	width   int // terminal width (for clamping)
	height  int // terminal height (for clamping)
}

// New creates a hidden context menu.
func New() Model {
	return Model{cursor: 0}
}

// Show displays the menu at the given position with the given items.
func (m *Model) Show(x, y int, items []Item) {
	m.items = items
	m.cursor = 0
	m.visible = true

	// Clamp position so menu doesn't overflow terminal.
	menuW := m.renderedWidth()
	menuH := m.renderedHeight()
	if x+menuW > m.width {
		x = m.width - menuW
	}
	if y+menuH > m.height {
		y = m.height - menuH
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	m.x = x
	m.y = y
}

// Hide dismisses the menu.
func (m *Model) Hide() {
	m.visible = false
}

// IsVisible returns whether the menu is shown.
func (m Model) IsVisible() bool {
	return m.visible
}

// SetSize updates the terminal dimensions for clamping.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m Model) menuWidth() int {
	maxLen := 0
	for _, item := range m.items {
		if len(item.Label) > maxLen {
			maxLen = len(item.Label)
		}
	}
	return maxLen + 4 // padding + border
}

// Update handles keyboard and mouse input for the context menu.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.visible = false
			return m, func() tea.Msg { return DismissMsg{} }
		case "j", "down":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.items) {
				m.visible = false
				action := m.items[m.cursor].Action
				return m, func() tea.Msg { return SelectMsg{Action: action} }
			}
		}

	case tea.MouseMsg:
		switch {
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
			menuW := m.renderedWidth()
			menuH := m.renderedHeight()
			if msg.X >= m.x && msg.X < m.x+menuW && msg.Y >= m.y && msg.Y < m.y+menuH {
				row := msg.Y - m.y - 1 // -1 for top border
				if row >= 0 && row < len(m.items) {
					m.visible = false
					action := m.items[row].Action
					return m, func() tea.Msg { return SelectMsg{Action: action} }
				}
			} else {
				m.visible = false
				return m, func() tea.Msg { return DismissMsg{} }
			}

		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		}
	}

	return m, nil
}

// renderBox builds the styled menu box and returns it along with its rendered dimensions.
func (m Model) renderBox() (box string, boxW, boxH int) {
	var rows []string
	itemWidth := m.menuWidth() - 4 // inner width (minus border + padding)
	for i, item := range m.items {
		label := item.Label
		pad := itemWidth - lipgloss.Width(label)
		if pad < 0 {
			pad = 0
		}
		line := " " + label + strings.Repeat(" ", pad) + " "
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A3A5A")).
				Foreground(theme.White).
				Render(line)
		} else {
			line = lipgloss.NewStyle().
				Foreground(theme.White).
				Render(line)
		}
		rows = append(rows, line)
	}

	content := strings.Join(rows, "\n")
	box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Render(content)

	boxLines := strings.Split(box, "\n")
	boxH = len(boxLines)
	for _, l := range boxLines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}
	return box, boxW, boxH
}

// renderedWidth returns the actual rendered width of the menu box (including borders).
func (m Model) renderedWidth() int {
	_, w, _ := m.renderBox()
	return w
}

// renderedHeight returns the actual rendered height of the menu box (including borders).
func (m Model) renderedHeight() int {
	_, _, h := m.renderBox()
	return h
}

// Overlay composites the context menu on top of a base view using ANSI-aware
// string splicing. Each line of the base that intersects the menu box is split
// into left | menu | right using ansi.Truncate and ansi.TruncateLeft so that
// escape codes are preserved correctly.
func (m Model) Overlay(base string) string {
	if !m.visible || len(m.items) == 0 {
		return base
	}

	box, _, boxH := m.renderBox()
	boxLines := strings.Split(box, "\n")
	baseLines := strings.Split(base, "\n")

	for len(baseLines) < m.height {
		baseLines = append(baseLines, "")
	}

	for i := 0; i < boxH && m.y+i < len(baseLines); i++ {
		bl := boxLines[i]
		blW := lipgloss.Width(bl)
		row := m.y + i
		baseLine := baseLines[row]

		// Left: first m.x columns of the base line (ANSI-aware).
		left := ansi.Truncate(baseLine, m.x, "")

		// Pad left if base line is shorter than m.x.
		leftW := lipgloss.Width(left)
		if leftW < m.x {
			left += strings.Repeat(" ", m.x-leftW)
		}

		// Right: everything past m.x + menu width in the base line.
		right := ansi.TruncateLeft(baseLine, m.x+blW, "")

		baseLines[row] = left + bl + right
	}

	return strings.Join(baseLines, "\n")
}
