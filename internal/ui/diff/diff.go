// internal/ui/diff/diff.go
package diff

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andrewstuart/ferry/internal/transfer"
	"github.com/andrewstuart/ferry/internal/ui/theme"
)

// SyncStartMsg carries the comparison result to the diff view.
type SyncStartMsg struct {
	Entries  []transfer.DiffEntry
	HasRsync bool
}

// SyncCompleteMsg signals that a sync operation finished.
type SyncCompleteMsg struct {
	Err error
}

// SyncAction describes the direction of a selected sync operation.
type SyncAction struct {
	Entries   []transfer.DiffEntry
	Direction string // "push" or "pull"
}

// Model is the Bubble Tea model for the sync/diff view.
type Model struct {
	entries  []transfer.DiffEntry
	cursor   int
	offset   int
	selected map[int]bool
	width    int
	height   int
	hasRsync bool
}

// New creates a new empty diff view model.
func New() Model {
	return Model{
		selected: make(map[int]bool),
	}
}

// SetEntries replaces the diff entries and resets navigation.
func (m *Model) SetEntries(entries []transfer.DiffEntry, hasRsync bool) {
	m.entries = entries
	m.hasRsync = hasRsync
	m.cursor = 0
	m.offset = 0
	m.selected = make(map[int]bool)
}

// SetSize updates the available terminal size.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SelectedEntries returns the diff entries currently selected (or the cursor
// entry if nothing is explicitly selected).
func (m *Model) SelectedEntries() []transfer.DiffEntry {
	var out []transfer.DiffEntry
	for idx := range m.selected {
		if idx >= 0 && idx < len(m.entries) {
			out = append(out, m.entries[idx])
		}
	}
	if len(out) == 0 && m.cursor >= 0 && m.cursor < len(m.entries) {
		out = append(out, m.entries[m.cursor])
	}
	return out
}

// Update handles keyboard input for the diff view.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
				m.ensureVisible()
			}

		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}

		case " ":
			// Toggle selection on current entry.
			if m.cursor >= 0 && m.cursor < len(m.entries) {
				if m.selected[m.cursor] {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = true
				}
			}

		case "a":
			// Select all non-same entries.
			m.selected = make(map[int]bool)
			for i, e := range m.entries {
				if e.Status != transfer.DiffSame {
					m.selected[i] = true
				}
			}

		case "right", "l":
			// Push selected to remote.
			sel := m.SelectedEntries()
			if len(sel) > 0 {
				return m, func() tea.Msg {
					return SyncAction{Entries: sel, Direction: "push"}
				}
			}

		case "left", "h":
			// Pull selected to local.
			sel := m.SelectedEntries()
			if len(sel) > 0 {
				return m, func() tea.Msg {
					return SyncAction{Entries: sel, Direction: "pull"}
				}
			}
		}
	}

	return m, nil
}

// ensureVisible adjusts the scroll offset so the cursor is within the viewport.
func (m *Model) ensureVisible() {
	viewHeight := m.viewportHeight()
	if viewHeight <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+viewHeight {
		m.offset = m.cursor - viewHeight + 1
	}
}

// viewportHeight returns how many entry rows fit on screen (reserve space for
// header + footer).
func (m *Model) viewportHeight() int {
	h := m.height - 4 // 1 title + 1 header + 1 footer + 1 border
	if h < 1 {
		h = 1
	}
	return h
}

// View renders the diff view.
func (m Model) View() string {
	if len(m.entries) == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(theme.Dim).Render("No differences found"))
	}

	var lines []string

	// Title.
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Cyan)
	selCount := len(m.selected)
	diffCount := 0
	for _, e := range m.entries {
		if e.Status != transfer.DiffSame {
			diffCount++
		}
	}
	title := titleStyle.Render(fmt.Sprintf(" Sync View  %d entries, %d differ, %d selected", len(m.entries), diffCount, selCount))
	lines = append(lines, title)

	// Column header.
	headerStyle := lipgloss.NewStyle().Foreground(theme.Dim)
	lines = append(lines, headerStyle.Render("  St  Name                                  Size       Side"))

	// Entries in viewport.
	vh := m.viewportHeight()
	end := m.offset + vh
	if end > len(m.entries) {
		end = len(m.entries)
	}

	for i := m.offset; i < end; i++ {
		e := m.entries[i]
		line := m.renderEntry(i, e)
		lines = append(lines, line)
	}

	// Footer with keybindings.
	footerStyle := lipgloss.NewStyle().Foreground(theme.Dim)
	footer := footerStyle.Render("  j/k:nav  Space:select  a:all  l/→:push  h/←:pull  Esc:back")
	lines = append(lines, footer)

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Width(m.width - 2).
		Height(m.height - 2).
		Render(content)

	return box
}

func (m Model) renderEntry(idx int, e transfer.DiffEntry) string {
	// Cursor / selection prefix.
	prefix := "  "
	if idx == m.cursor && m.selected[idx] {
		prefix = "»*"
	} else if idx == m.cursor {
		prefix = "» "
	} else if m.selected[idx] {
		prefix = " *"
	}

	// Status icon.
	var icon string
	var iconStyle lipgloss.Style
	switch e.Status {
	case transfer.DiffSame:
		icon = "[=]"
		iconStyle = lipgloss.NewStyle().Foreground(theme.Dim)
	case transfer.DiffLocalOnly:
		icon = "[+]"
		iconStyle = lipgloss.NewStyle().Foreground(theme.Cyan)
	case transfer.DiffRemoteOnly:
		icon = "[-]"
		iconStyle = lipgloss.NewStyle().Foreground(theme.Red)
	case transfer.DiffModified:
		icon = "[M]"
		iconStyle = lipgloss.NewStyle().Foreground(theme.Amber)
	}

	// Name (with directory indicator).
	name := e.RelPath
	if e.IsDir {
		name += "/"
	}
	maxNameLen := 38
	if len(name) > maxNameLen {
		name = name[:maxNameLen-1] + "~"
	}
	name = fmt.Sprintf("%-*s", maxNameLen, name)

	// Size info.
	sizeStr := "         "
	if e.LocalEntry != nil && !e.IsDir {
		sizeStr = fmt.Sprintf("%9s", formatSize(e.LocalEntry.Size))
	} else if e.RemoteEntry != nil && !e.IsDir {
		sizeStr = fmt.Sprintf("%9s", formatSize(e.RemoteEntry.Size))
	}

	// Side info for modified entries.
	sideStr := "      "
	if e.Status == transfer.DiffModified && e.NewerSide != "" {
		if e.NewerSide == "local" {
			sideStr = lipgloss.NewStyle().Foreground(theme.Cyan).Render("local ")
		} else {
			sideStr = lipgloss.NewStyle().Foreground(theme.Amber).Render("remote")
		}
	}

	line := fmt.Sprintf("%s %s %s %s %s", prefix, iconStyle.Render(icon), name, sizeStr, sideStr)

	// Highlight cursor row.
	if idx == m.cursor {
		line = lipgloss.NewStyle().
			Background(lipgloss.Color("#2A3A5A")).
			Foreground(theme.White).
			Render(line)
	}

	return line
}

// formatSize returns a human-readable size string.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
