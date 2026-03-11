// internal/ui/diff/diff.go
package diff

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/HopperShell/ferry/internal/transfer"
	"github.com/HopperShell/ferry/internal/ui/theme"
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

// MirrorAction requests a full mirror in the given direction.
type MirrorAction struct {
	Direction string // "push" or "pull"
}

// SyncProgressMsg updates the transfer progress counter in the diff view.
type SyncProgressMsg struct {
	Done  int
	Total int
	Name  string // item just completed
}

// SyncDoneMsg signals that all transfers finished and a re-compare should start.
type SyncDoneMsg struct{}

// SyncRefreshMsg carries refreshed entries after all sync transfers complete.
type SyncRefreshMsg struct {
	Entries  []transfer.DiffEntry
	HasRsync bool
	Err      error
}

// Model is the Bubble Tea model for the sync/diff view.
type Model struct {
	entries    []transfer.DiffEntry // all entries (including same)
	cursor     int
	offset     int
	selected   map[int]bool
	width      int
	height     int
	hasRsync   bool
	scope      string // what path is being compared (shown in title)
	comparing     bool   // true while Compare is running
	syncing       bool   // true while a transfer is in progress
	syncStatus    string // status message during sync
	syncDone      int    // completed items
	syncTotal     int    // total items to transfer
	mirrorPending bool   // true after pressing M, waiting for direction
	confirmMsg    string // shown in footer when awaiting y/n confirmation
	lastClickTime time.Time
	lastClickRow  int
}

// New creates a new empty diff view model.
func New() Model {
	return Model{
		selected: make(map[int]bool),
	}
}

// SetComparing marks the view as busy running a directory comparison.
func (m *Model) SetComparing() {
	m.comparing = true
}

// SetEntries replaces the diff entries and resets navigation.
func (m *Model) SetEntries(entries []transfer.DiffEntry, hasRsync bool) {
	m.hasRsync = hasRsync
	m.cursor = 0
	m.offset = 0
	m.selected = make(map[int]bool)
	m.entries = entries
	m.comparing = false
	m.syncing = false
	m.syncStatus = ""
}

// SetSyncing marks the view as busy transferring.
func (m *Model) SetSyncing(status string) {
	m.syncing = true
	m.syncStatus = status
	m.selected = make(map[int]bool)
}

// SetSyncProgress updates the progress counter.
func (m *Model) SetSyncProgress(done, total int, name string) {
	m.syncing = true
	m.syncDone = done
	m.syncTotal = total
	m.syncStatus = name
}

// SetConfirmMsg sets a confirmation message shown in the footer.
// Pass "" to clear it.
func (m *Model) SetConfirmMsg(msg string) {
	m.confirmMsg = msg
}

// HasRsync returns whether the remote has rsync available.
func (m *Model) HasRsync() bool {
	return m.hasRsync
}

// DiffCount returns the number of non-same entries.
func (m *Model) DiffCount() int {
	n := 0
	for _, e := range m.entries {
		if e.Status != transfer.DiffSame {
			n++
		}
	}
	return n
}

// SetScope sets the path label shown in the title (e.g. "anotherdir2/").
func (m *Model) SetScope(scope string) {
	m.scope = scope
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

// diffEntries returns only entries that differ.
func (m Model) diffEntries() []transfer.DiffEntry {
	var out []transfer.DiffEntry
	for _, e := range m.entries {
		if e.Status != transfer.DiffSame {
			out = append(out, e)
		}
	}
	return out
}

// DiffEntries returns all entries (including same).
func (m Model) DiffEntries() []transfer.DiffEntry {
	return m.entries
}

// sameCount returns how many entries are in sync.
func (m Model) sameCount() int {
	n := 0
	for _, e := range m.entries {
		if e.Status == transfer.DiffSame {
			n++
		}
	}
	return n
}

// Update handles keyboard input for the diff view.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.syncing || m.comparing {
		return m, nil // ignore input while syncing or comparing
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		return m.updateMouse(msg)

	case tea.KeyMsg:
		// Handle mirror-pending state: waiting for direction after M.
		if m.mirrorPending {
			switch msg.String() {
			case "right", "l":
				m.mirrorPending = false
				return m, func() tea.Msg {
					return MirrorAction{Direction: "push"}
				}
			case "left", "h":
				m.mirrorPending = false
				return m, func() tea.Msg {
					return MirrorAction{Direction: "pull"}
				}
			case "esc":
				m.mirrorPending = false
			default:
				m.mirrorPending = false
			}
			return m, nil
		}

		visible := m.diffEntries()
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(visible)-1 {
				m.cursor++
				m.ensureVisible()
			}

		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}

		case " ":
			if m.cursor >= 0 && m.cursor < len(visible) {
				if m.selected[m.cursor] {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = true
				}
			}

		case "a":
			m.selected = make(map[int]bool)
			for i := range visible {
				m.selected[i] = true
			}

		case "M":
			m.mirrorPending = true
			return m, nil

		case "right", "l":
			sel := m.selectedFromVisible(visible)
			if len(sel) > 0 {
				return m, func() tea.Msg {
					return SyncAction{Entries: sel, Direction: "push"}
				}
			}

		case "left", "h":
			sel := m.selectedFromVisible(visible)
			if len(sel) > 0 {
				return m, func() tea.Msg {
					return SyncAction{Entries: sel, Direction: "pull"}
				}
			}
		}
	}

	return m, nil
}

// selectedFromVisible returns the diff entries selected by index in the visible (non-same) list.
func (m *Model) selectedFromVisible(visible []transfer.DiffEntry) []transfer.DiffEntry {
	var out []transfer.DiffEntry
	for idx := range m.selected {
		if idx >= 0 && idx < len(visible) {
			out = append(out, visible[idx])
		}
	}
	if len(out) == 0 && m.cursor >= 0 && m.cursor < len(visible) {
		out = append(out, visible[m.cursor])
	}
	return out
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

// ContextMenuMsg is emitted on right-click to request a context menu.
type ContextMenuMsg struct {
	X, Y int
}

func (m Model) updateMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	visible := m.diffEntries()

	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
		case tea.MouseButtonWheelDown:
			if m.cursor < len(visible)-1 {
				m.cursor++
				m.ensureVisible()
			}
		case tea.MouseButtonLeft:
			// View layout: border(1) + title(1) + header(1) = 3 rows before entries.
			entryStartY := 3
			row := msg.Y - entryStartY + m.offset
			if row < 0 || row >= len(visible) {
				return m, nil
			}

			now := time.Now()
			isDouble := row == m.lastClickRow && now.Sub(m.lastClickTime) < 400*time.Millisecond
			m.lastClickTime = now
			m.lastClickRow = row

			if isDouble {
				// Double-click: toggle selection.
				if m.selected[row] {
					delete(m.selected, row)
				} else {
					m.selected[row] = true
				}
			} else {
				m.cursor = row
				m.ensureVisible()
			}

		case tea.MouseButtonRight:
			entryStartY := 3
			row := msg.Y - entryStartY + m.offset
			if row >= 0 && row < len(visible) {
				m.cursor = row
				m.ensureVisible()
				return m, func() tea.Msg {
					return ContextMenuMsg{X: msg.X, Y: msg.Y}
				}
			}
		}
	}

	return m, nil
}

// Select marks the given index as selected.
func (m *Model) Select(idx int) {
	m.selected[idx] = true
}

// ClearSelection removes all selections.
func (m *Model) ClearSelection() {
	m.selected = make(map[int]bool)
}

// viewportHeight returns how many entry rows fit on screen.
func (m *Model) viewportHeight() int {
	h := m.height - 5
	if h < 1 {
		h = 1
	}
	return h
}

// View renders the diff view.
func (m Model) View() string {
	visible := m.diffEntries()
	same := m.sameCount()

	scopeLabel := ""
	if m.scope != "" {
		scopeLabel = " " + m.scope
	}

	if m.comparing {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(theme.Cyan).Render("⠋ Comparing directories..."))
	}

	if len(visible) == 0 && !m.syncing {
		msg := fmt.Sprintf("All %d entries in%s are in sync", len(m.entries), scopeLabel)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(theme.Green).Render(msg)+"\n"+
				lipgloss.NewStyle().Foreground(theme.Dim).Render("Esc to go back"))
	}

	var lines []string

	// Title.
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Cyan)
	selCount := len(m.selected)
	titleText := fmt.Sprintf(" Sync%s  %d differ, %d selected", scopeLabel, len(visible), selCount)
	if m.syncing && m.syncTotal > 0 {
		barWidth := 20
		pct := float64(m.syncDone) / float64(m.syncTotal)
		filled := int(pct * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", barWidth-filled)
		titleText = fmt.Sprintf(" Sync%s  %s %d/%d  %s", scopeLabel, bar, m.syncDone, m.syncTotal, m.syncStatus)
	} else if m.syncing && m.syncDone > 0 {
		titleText = fmt.Sprintf(" Sync%s  %d files  %s", scopeLabel, m.syncDone, m.syncStatus)
	} else if m.syncing {
		titleText = fmt.Sprintf(" Sync%s  %s", scopeLabel, m.syncStatus)
	}
	lines = append(lines, titleStyle.Render(titleText))

	// Column header.
	headerStyle := lipgloss.NewStyle().Foreground(theme.Dim)
	lines = append(lines, headerStyle.Render("       Status        Name                              Size"))

	// Entries in viewport.
	vh := m.viewportHeight()
	end := m.offset + vh
	if end > len(visible) {
		end = len(visible)
	}

	for i := m.offset; i < end; i++ {
		e := visible[i]
		line := m.renderEntry(i, e)
		lines = append(lines, line)
	}

	// Same-count summary.
	if same > 0 {
		sameStyle := lipgloss.NewStyle().Foreground(theme.Dim).Italic(true)
		lines = append(lines, sameStyle.Render(fmt.Sprintf("  (%d entries in sync, not shown)", same)))
	}

	// Footer with keybindings.
	footerStyle := lipgloss.NewStyle().Foreground(theme.Dim)
	if m.confirmMsg != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Amber).Bold(true).Render("  "+m.confirmMsg))
	} else if m.syncing {
		lines = append(lines, footerStyle.Render("  Transferring..."))
	} else if m.mirrorPending {
		lines = append(lines, lipgloss.NewStyle().Foreground(theme.Amber).Bold(true).Render("  Press → to mirror-push to remote, ← to mirror-pull to local, Esc to cancel"))
	} else {
		pushHint := lipgloss.NewStyle().Foreground(theme.Cyan).Render("→ push to remote")
		pullHint := lipgloss.NewStyle().Foreground(theme.Amber).Render("← pull to local")
		mirrorHint := footerStyle.Render("M+→/← mirror")
		footer := footerStyle.Render("  j/k:nav  Space:select  a:all  ") + pushHint + footerStyle.Render("  ") + pullHint + footerStyle.Render("  ") + mirrorHint + footerStyle.Render("  Esc:back")
		lines = append(lines, footer)
	}

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
	if !m.syncing {
		if idx == m.cursor && m.selected[idx] {
			prefix = "»*"
		} else if idx == m.cursor {
			prefix = "» "
		} else if m.selected[idx] {
			prefix = " *"
		}
	}

	// Status label.
	var label string
	var labelStyle lipgloss.Style
	switch e.Status {
	case transfer.DiffLocalOnly:
		label = "local → "
		labelStyle = lipgloss.NewStyle().Foreground(theme.Cyan)
	case transfer.DiffRemoteOnly:
		label = "← remote"
		labelStyle = lipgloss.NewStyle().Foreground(theme.Amber)
	case transfer.DiffModified:
		if e.NewerSide == "local" {
			label = "local ≠ "
			labelStyle = lipgloss.NewStyle().Foreground(theme.Cyan)
		} else {
			label = " ≠ remote"
			labelStyle = lipgloss.NewStyle().Foreground(theme.Amber)
		}
	default:
		label = "  same  "
		labelStyle = lipgloss.NewStyle().Foreground(theme.Dim)
	}

	// Name (with directory indicator).
	name := e.RelPath
	if e.IsDir {
		name += "/"
	}
	maxNameLen := 34
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

	line := fmt.Sprintf("%s %s %s %s", prefix, labelStyle.Render(label), name, sizeStr)

	// Highlight cursor row.
	if !m.syncing && idx == m.cursor {
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
