package pane

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/HopperShell/ferry/internal/fs"
	"github.com/HopperShell/ferry/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// Messages for async directory listing. The label field identifies which pane
// the message belongs to, so messages routed to both panes are only accepted
// by the correct one.
type entriesMsg struct {
	label   string
	path    string
	entries []fs.Entry
}

type errMsg struct {
	label string
	err   error
}

// findResultsMsg delivers a batch of recursive walk results to the pane.
type findResultsMsg struct {
	label   string
	results []fs.WalkResult
	done    bool // true when the walk has completed
}

// TransferRequestMsg is emitted when the user presses Enter on a file (or with
// files selected). The parent model handles this by transferring the entries to
// the other pane.
type TransferRequestMsg struct {
	Entries []fs.Entry
}

// SortMode determines how entries are sorted within the pane.
type SortMode int

const (
	SortByName SortMode = iota
	SortBySize
	SortByDate
)

func (s SortMode) String() string {
	switch s {
	case SortBySize:
		return "size"
	case SortByDate:
		return "date"
	default:
		return "name"
	}
}

// Model is a file browser pane component. It is NOT an independent Bubble Tea
// program; its Update and View methods are called by the parent.
type Model struct {
	fs          fs.FileSystem
	label       string // "Local" or "Remote"
	path        string // current directory path
	entries     []fs.Entry
	loaded      bool   // whether the first listing has completed
	cursor      int
	offset      int // scroll offset for viewport
	selected    map[string]bool
	showHidden  bool
	search      bool // whether search mode is active
	searchInput textinput.Model
	filtered    []int // indices into entries that match search
	width       int
	height      int
	err         error
	anchor      int      // for range select (V)
	lastKey     string   // for gg detection
	active      bool     // whether this pane is the focused pane
	sortMode    SortMode // current sort order

	// Recursive find (Ctrl+f) state.
	find         bool                   // whether find mode is active
	findInput    textinput.Model        // search text input
	findResults  []fs.WalkResult        // accumulated walk results
	findFiltered []int                  // indices into findResults matching query
	findCursor   int                    // cursor position in filtered results
	findOffset   int                    // scroll offset for find results
	findDone     bool                   // walk finished
	findCancel   context.CancelFunc     // cancels the walk goroutine
	findCh       <-chan fs.WalkResult   // channel for receiving walk results

	pendingHighlight string // filename to highlight after next entriesMsg
}

// New creates a new pane backed by the given filesystem.
func New(filesystem fs.FileSystem, label string) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 256

	fi := textinput.New()
	fi.Prompt = "find: "
	fi.CharLimit = 256

	return Model{
		fs:          filesystem,
		label:       label,
		path:        "/",
		selected:    make(map[string]bool),
		searchInput: ti,
		findInput:   fi,
		anchor:      -1,
	}
}

// SetSize is called by the parent on terminal resize.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetActive marks whether this pane currently has focus.
func (m *Model) SetActive(active bool) {
	m.active = active
}

// InputActive reports whether the pane is in a mode that captures text input
// (e.g. search or find). The parent should skip app-level key handling when
// this returns true.
func (m Model) InputActive() bool {
	return m.search || m.find
}

// CurrentEntry returns the entry under the cursor, or nil.
func (m Model) CurrentEntry() *fs.Entry {
	idx := m.visibleIndex()
	if idx < 0 || idx >= len(m.entries) {
		return nil
	}
	e := m.entries[idx]
	return &e
}

// SelectedEntries returns all explicitly selected entries.
// If nothing is selected, returns the entry under the cursor.
func (m Model) SelectedEntries() []fs.Entry {
	var out []fs.Entry
	for _, e := range m.entries {
		if m.selected[e.Path] {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		if ce := m.CurrentEntry(); ce != nil {
			out = append(out, *ce)
		}
	}
	return out
}

// Path returns the current directory path.
func (m Model) Path() string {
	return m.path
}

// FS returns the filesystem backing this pane.
func (m Model) FS() fs.FileSystem {
	return m.fs
}

// Refresh re-lists the current directory.
func (m Model) Refresh() tea.Cmd {
	return m.listDir(m.path)
}

// NavigateTo changes to the specified directory.
func (m *Model) NavigateTo(path string) tea.Cmd {
	m.path = path
	return m.listDir(path)
}

// Init loads the initial directory (home or root).
func (m Model) Init() tea.Cmd {
	label := m.label
	return func() tea.Msg {
		home, err := m.fs.HomeDir()
		if err != nil {
			home = "/"
		}
		entries, err := m.fs.List(home)
		if err != nil {
			return errMsg{label: label, err: err}
		}
		return entriesMsg{label: label, path: home, entries: entries}
	}
}

// Update handles key messages. It is called by the parent.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case entriesMsg:
		if msg.label != m.label {
			return m, nil // not for this pane
		}
		m.path = msg.path
		m.entries = sortEntries(msg.entries, m.sortMode)
		m.loaded = true
		m.cursor = 0
		m.offset = 0
		m.err = nil
		m.filtered = nil
		m.search = false
		m.searchInput.SetValue("")
		m.applyFilters()
		// If we're jumping to a file from find mode, highlight it.
		if m.pendingHighlight != "" {
			name := m.pendingHighlight
			m.pendingHighlight = ""
			for i := 0; i < m.visibleCount(); i++ {
				idx := m.mapIndex(i)
				if idx >= 0 && idx < len(m.entries) && m.entries[idx].Name == name {
					m.cursor = i
					break
				}
			}
		}
		m.clampCursor()
		m.ensureVisible()
		return m, nil

	case findResultsMsg:
		if msg.label != m.label {
			return m, nil
		}
		if !m.find {
			return m, nil // find was cancelled, ignore stale results
		}
		m.findResults = append(m.findResults, msg.results...)
		m.findDone = msg.done
		m.applyFindFilter()
		if !msg.done {
			return m, m.readWalkBatch()
		}
		return m, nil

	case errMsg:
		if msg.label != m.label {
			return m, nil // not for this pane
		}
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		if m.find {
			return m.updateFind(msg)
		}
		// In search mode, most keys go to the text input.
		if m.search {
			return m.updateSearch(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m Model) updateSearch(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.search = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.filtered = nil
		m.cursor = 0
		m.offset = 0
		m.applyFilters()
		return m, nil
	case "enter":
		// Accept the filter and leave search mode but keep the filter.
		m.search = false
		m.searchInput.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.applySearch()
	return m, cmd
}

func (m Model) updateNormal(msg tea.KeyMsg) (Model, tea.Cmd) {
	key := msg.String()
	defer func() { m.lastKey = key }()

	visCount := m.visibleCount()

	switch key {
	case "j", "down":
		if m.cursor < visCount-1 {
			m.cursor++
		}
		m.ensureVisible()

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.ensureVisible()

	case "g":
		if m.lastKey == "g" {
			m.cursor = 0
			m.offset = 0
			m.lastKey = ""
			return m, nil
		}
		// Wait for another g.
		return m, nil

	case "G":
		if visCount > 0 {
			m.cursor = visCount - 1
		}
		m.ensureVisible()

	case "ctrl+d":
		pageSize := m.listHeight() / 2
		if pageSize < 1 {
			pageSize = 1
		}
		m.cursor += pageSize
		if m.cursor >= visCount {
			m.cursor = visCount - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureVisible()

	case "ctrl+u":
		pageSize := m.listHeight() / 2
		if pageSize < 1 {
			pageSize = 1
		}
		m.cursor -= pageSize
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.ensureVisible()

	case "l", "right", "enter":
		// Enter with explicit selections: transfer everything selected.
		if key == "enter" && len(m.selected) > 0 {
			entries := m.SelectedEntries()
			return m, func() tea.Msg {
				return TransferRequestMsg{Entries: entries}
			}
		}
		// Navigate into directory.
		if e := m.CurrentEntry(); e != nil && e.IsDir {
			target := e.Path
			m.selected = make(map[string]bool)
			m.anchor = -1
			return m, m.listDir(target)
		}
		// Enter on a single file (no selections): transfer it.
		if key == "enter" {
			if e := m.CurrentEntry(); e != nil {
				return m, func() tea.Msg {
					return TransferRequestMsg{Entries: []fs.Entry{*e}}
				}
			}
		}

	case "h", "left", "backspace":
		parent := filepath.Dir(m.path)
		if parent != m.path {
			m.selected = make(map[string]bool)
			m.anchor = -1
			return m, m.listDir(parent)
		}

	case " ":
		if e := m.CurrentEntry(); e != nil {
			if m.selected[e.Path] {
				delete(m.selected, e.Path)
			} else {
				m.selected[e.Path] = true
			}
			m.anchor = m.cursor
			// Move cursor down after toggling.
			if m.cursor < visCount-1 {
				m.cursor++
			}
			m.ensureVisible()
		}

	case "V":
		if m.anchor < 0 {
			m.anchor = m.cursor
		}
		lo, hi := m.anchor, m.cursor
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			idx := m.mapIndex(i)
			if idx >= 0 && idx < len(m.entries) {
				m.selected[m.entries[idx].Path] = true
			}
		}

	case "H":
		m.showHidden = !m.showHidden
		m.cursor = 0
		m.offset = 0
		m.applyFilters()

	case "s":
		m.sortMode = (m.sortMode + 1) % 3
		m.entries = sortEntries(m.entries, m.sortMode)
		m.applyFilters()
		m.clampCursor()

	case "/":
		m.search = true
		m.searchInput.Focus()
		return m, textinput.Blink

	case "ctrl+f":
		return m.startFind()
	}

	return m, nil
}

// --- Recursive find (Ctrl+f) ---

func (m Model) startFind() (Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan fs.WalkResult, 64)
	go fs.Walk(ctx, m.fs, m.path, ch)

	m.find = true
	m.findResults = nil
	m.findFiltered = nil
	m.findCursor = 0
	m.findOffset = 0
	m.findDone = false
	m.findCancel = cancel
	m.findCh = ch
	m.findInput.SetValue("")
	m.findInput.Focus()

	return m, tea.Batch(textinput.Blink, m.readWalkBatch())
}

// readWalkBatch returns a Cmd that reads up to 50 results from the walk channel.
func (m Model) readWalkBatch() tea.Cmd {
	ch := m.findCh
	label := m.label
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		var batch []fs.WalkResult
		for {
			r, ok := <-ch
			if !ok {
				return findResultsMsg{label: label, results: batch, done: true}
			}
			batch = append(batch, r)
			if len(batch) >= 50 {
				return findResultsMsg{label: label, results: batch, done: false}
			}
		}
	}
}

func (m Model) updateFind(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.findCancel != nil {
			m.findCancel()
		}
		m.find = false
		m.findInput.Blur()
		m.findResults = nil
		m.findFiltered = nil
		return m, nil

	case "enter":
		if len(m.findFiltered) > 0 && m.findCursor >= 0 && m.findCursor < len(m.findFiltered) {
			r := m.findResults[m.findFiltered[m.findCursor]]
			if m.findCancel != nil {
				m.findCancel()
			}
			m.find = false
			m.findInput.Blur()
			m.pendingHighlight = r.Entry.Name
			// Navigate to the directory containing this entry.
			return m, m.listDir(r.Dir)
		}
		return m, nil

	case "up", "ctrl+p":
		if m.findCursor > 0 {
			m.findCursor--
			m.ensureFindVisible()
		}
		return m, nil

	case "down", "ctrl+n":
		if m.findCursor < len(m.findFiltered)-1 {
			m.findCursor++
			m.ensureFindVisible()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.findInput, cmd = m.findInput.Update(msg)
	m.applyFindFilter()
	return m, cmd
}

func (m *Model) applyFindFilter() {
	query := m.findInput.Value()
	if query == "" {
		// Show all results.
		m.findFiltered = make([]int, len(m.findResults))
		for i := range m.findResults {
			m.findFiltered[i] = i
		}
		m.findCursor = 0
		m.findOffset = 0
		return
	}

	candidates := make([]string, len(m.findResults))
	for i, r := range m.findResults {
		candidates[i] = r.Entry.Name
	}

	matches := fuzzy.Find(query, candidates)
	m.findFiltered = nil
	for _, match := range matches {
		m.findFiltered = append(m.findFiltered, match.Index)
	}
	m.findCursor = 0
	m.findOffset = 0
}

func (m *Model) ensureFindVisible() {
	lh := m.listHeight()
	if m.findCursor < m.findOffset {
		m.findOffset = m.findCursor
	}
	if m.findCursor >= m.findOffset+lh {
		m.findOffset = m.findCursor - lh + 1
	}
}

// View renders the pane.
func (m Model) View() string {
	contentWidth := m.width - 2 // account for border
	if contentWidth < 10 {
		contentWidth = 10
	}
	listH := m.listHeight()

	// Header: label + path.
	title := theme.TitleStyle.Render(m.label + ": " + m.shortenPath(contentWidth-4))

	var body, footer string

	if m.find {
		// Find mode: show input + filtered results dropdown.
		var rows []string
		for i := m.findOffset; i < m.findOffset+listH && i < len(m.findFiltered); i++ {
			idx := m.findFiltered[i]
			if idx < 0 || idx >= len(m.findResults) {
				continue
			}
			r := m.findResults[idx]
			name := r.Entry.Name
			if r.Entry.IsDir {
				name += "/"
			}
			// Show relative path from current directory.
			relDir := strings.TrimPrefix(r.Dir, m.path)
			relDir = strings.TrimPrefix(relDir, "/")
			var display string
			if relDir != "" {
				display = relDir + "/" + name
			} else {
				display = name
			}
			if len(display) > contentWidth {
				display = display[:contentWidth-1] + "\u2026"
			}

			var styled string
			if r.Entry.IsDir {
				styled = theme.DirStyle.Render(display)
			} else {
				styled = theme.FileStyle.Render(display)
			}
			// Pad to full width.
			pad := contentWidth - lipgloss.Width(styled)
			if pad > 0 {
				styled += strings.Repeat(" ", pad)
			}
			if i == m.findCursor {
				styled = theme.CursorStyle.Render(styled)
			}
			rows = append(rows, styled)
		}
		if len(rows) == 0 {
			msg := "(no results)"
			if !m.findDone {
				msg = "(searching...)"
			}
			rows = append(rows, lipgloss.NewStyle().Foreground(theme.Dim).Italic(true).Render(msg))
		}
		for len(rows) < listH {
			rows = append(rows, strings.Repeat(" ", contentWidth))
		}
		body = strings.Join(rows, "\n")

		// Footer: input + status.
		status := fmt.Sprintf(" %d results", len(m.findFiltered))
		if !m.findDone {
			status += " (searching...)"
		}
		footer = m.findInput.View() + lipgloss.NewStyle().Foreground(theme.Dim).Render(status)
	} else {
		// Normal mode: file list.
		var rows []string
		visCount := m.visibleCount()

		if !m.loaded {
			loadingMsg := lipgloss.NewStyle().Foreground(theme.Dim).Italic(true).Render("Loading...")
			rows = append(rows, loadingMsg)
		} else if visCount == 0 {
			emptyMsg := lipgloss.NewStyle().Foreground(theme.Dim).Italic(true).Render("(empty)")
			rows = append(rows, emptyMsg)
		} else {
			for i := m.offset; i < m.offset+listH && i < visCount; i++ {
				idx := m.mapIndex(i)
				if idx < 0 || idx >= len(m.entries) {
					continue
				}
				e := m.entries[idx]
				row := m.renderRow(e, i, contentWidth)
				rows = append(rows, row)
			}
		}
		for len(rows) < listH {
			rows = append(rows, strings.Repeat(" ", contentWidth))
		}
		body = strings.Join(rows, "\n")

		if m.search {
			footer = m.searchInput.View()
		} else if m.err != nil {
			footer = theme.ErrorStyle.Render(m.err.Error())
		} else {
			selCount := len(m.selected)
			info := fmt.Sprintf(" %d items", visCount)
			if selCount > 0 {
				var totalSize int64
				for _, e := range m.entries {
					if m.selected[e.Path] && !e.IsDir {
						totalSize += e.Size
					}
				}
				info += fmt.Sprintf(" | %d selected (%s)", selCount, formatSize(totalSize))
			}
			if m.sortMode != SortByName {
				info += fmt.Sprintf(" | sort: %s", m.sortMode)
			}
			footer = lipgloss.NewStyle().Foreground(theme.Dim).Render(info)
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, body, footer)

	// Border.
	var border lipgloss.Style
	if m.active {
		border = theme.ActiveBorder.Copy().Width(m.width - 2).Height(m.height - 2)
	} else {
		border = theme.InactiveBorder.Copy().Width(m.width - 2).Height(m.height - 2)
	}

	return border.Render(content)
}

// --- Internal helpers ---

func (m Model) listDir(path string) tea.Cmd {
	prevPath := m.path
	label := m.label
	return func() tea.Msg {
		entries, err := m.fs.List(path)
		if err != nil {
			if path != prevPath {
				return errMsg{label: label, err: fmt.Errorf("%s: %w", filepath.Base(path), err)}
			}
			return errMsg{label: label, err: err}
		}
		return entriesMsg{label: label, path: path, entries: entries}
	}
}

func sortEntries(entries []fs.Entry, mode SortMode) []fs.Entry {
	sort.SliceStable(entries, func(i, j int) bool {
		// Directories first, always.
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		switch mode {
		case SortBySize:
			return entries[i].Size > entries[j].Size
		case SortByDate:
			return entries[i].ModTime.After(entries[j].ModTime)
		default:
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		}
	})
	return entries
}

func (m *Model) applyFilters() {
	m.filtered = nil
	for i, e := range m.entries {
		if !m.showHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		m.filtered = append(m.filtered, i)
	}
}

func (m *Model) applySearch() {
	query := m.searchInput.Value()
	if query == "" {
		m.applyFilters()
		m.cursor = 0
		m.offset = 0
		return
	}

	// Build candidate list from visible (hidden-filtered) entries.
	var candidates []string
	var candidateIndices []int
	for i, e := range m.entries {
		if !m.showHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		candidates = append(candidates, e.Name)
		candidateIndices = append(candidateIndices, i)
	}

	matches := fuzzy.Find(query, candidates)
	m.filtered = nil
	for _, match := range matches {
		m.filtered = append(m.filtered, candidateIndices[match.Index])
	}
	m.cursor = 0
	m.offset = 0
}

func (m Model) visibleCount() int {
	if m.filtered != nil {
		return len(m.filtered)
	}
	return len(m.entries)
}

// visibleIndex maps the cursor position to an index in m.entries.
func (m Model) visibleIndex() int {
	return m.mapIndex(m.cursor)
}

func (m Model) mapIndex(pos int) int {
	if m.filtered != nil {
		if pos < 0 || pos >= len(m.filtered) {
			return -1
		}
		return m.filtered[pos]
	}
	return pos
}

func (m Model) listHeight() int {
	// Total height minus header (1), footer (1), and border (2 top/bottom).
	h := m.height - 4
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) ensureVisible() {
	lh := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+lh {
		m.offset = m.cursor - lh + 1
	}
}

// clampCursor ensures the cursor stays within valid bounds.
func (m *Model) clampCursor() {
	vc := m.visibleCount()
	if vc == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= vc {
		m.cursor = vc - 1
	}
}

func (m Model) renderRow(e fs.Entry, cursorPos int, width int) string {
	isCursor := cursorPos == m.cursor
	isSel := m.selected[e.Path]

	// Columns: name, size, mtime.
	nameWidth := width - 10 - 14 // 10 for size, 14 for date, some padding
	if nameWidth < 10 {
		nameWidth = 10
	}

	name := e.Name
	if e.IsDir {
		name += "/"
	}
	if len(name) > nameWidth {
		name = name[:nameWidth-1] + "\u2026" // ellipsis character
	}

	size := formatSize(e.Size)
	if e.IsDir {
		size = ""
	}

	mtime := formatTime(e.ModTime)

	// Apply style to name based on type.
	var nameStyled string
	switch {
	case e.IsDir:
		nameStyled = theme.DirStyle.Render(name)
	case e.Mode&os.ModeSymlink != 0:
		nameStyled = theme.LinkStyle.Render(name)
	case e.Mode&0o111 != 0:
		nameStyled = theme.ExecStyle.Render(name)
	default:
		nameStyled = theme.FileStyle.Render(name)
	}

	sizeStyled := theme.SizeStyle.Width(9).Render(size)
	mtimeStyled := lipgloss.NewStyle().Foreground(theme.Dim).Render(mtime)

	// Pad name column.
	namePad := nameWidth - lipgloss.Width(nameStyled)
	if namePad < 0 {
		namePad = 0
	}
	row := nameStyled + strings.Repeat(" ", namePad) + " " + sizeStyled + " " + mtimeStyled

	// Ensure row fills width.
	rowWidth := lipgloss.Width(row)
	if rowWidth < width {
		row += strings.Repeat(" ", width-rowWidth)
	}

	// Apply cursor/selection highlight.
	switch {
	case isCursor && isSel:
		row = theme.SelectedStyle.Render(row)
	case isCursor:
		row = theme.CursorStyle.Render(row)
	case isSel:
		row = theme.SelectedStyle.Render(row)
	}

	return row
}

func (m Model) shortenPath(maxLen int) string {
	p := m.path
	if len(p) <= maxLen {
		return p
	}
	// Show .../<last components>.
	parts := strings.Split(p, "/")
	for len(parts) > 1 {
		parts = parts[1:]
		short := ".../" + strings.Join(parts, "/")
		if len(short) <= maxLen {
			return short
		}
	}
	return p[len(p)-maxLen:]
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatTime(t time.Time) string {
	sixMonthsAgo := time.Now().AddDate(0, -6, 0)
	if t.Before(sixMonthsAgo) {
		return t.Format("Jan 02  2006")
	}
	return t.Format("Jan 02 15:04")
}
