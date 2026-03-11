# Mouse Support Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add full mouse support to ferry — clicks, scrolling, double-click, drag-select, right-click context menus — across all views.

**Architecture:** Enable `tea.WithMouseCellMotion()` at the program level, then handle `tea.MouseMsg` in each view's Update method. Mouse coordinates are mapped to UI elements using the existing layout math (pane widths, offsets, list heights). A new context menu component renders a floating menu at the click position.

**Tech Stack:** Bubble Tea v1 `tea.MouseMsg` (Action, Button, X, Y fields), lipgloss for context menu rendering.

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `cmd/ferry/main.go` | Modify | Add `tea.WithMouseCellMotion()` |
| `internal/app/app.go` | Modify | Route `tea.MouseMsg` to correct component by state and coordinates |
| `internal/ui/pane/pane.go` | Modify | Handle mouse clicks, scroll, double-click, drag-select in file list |
| `internal/ui/picker/picker.go` | Modify | Handle mouse clicks and scroll in connection list |
| `internal/ui/diff/diff.go` | Modify | Handle mouse clicks, scroll, double-click toggle in sync view |
| `internal/ui/contextmenu/menu.go` | Create | New context menu component — floating menu rendered at position |
| `internal/ui/modal/help.go` | Modify | Dismiss on click outside |
| `internal/ui/statusbar/statusbar.go` | Modify | Handle click on connection info and error message |

---

## Chunk 1: Enable Mouse & Wire Up Routing

### Task 1: Enable mouse input at program level

**Files:**
- Modify: `cmd/ferry/main.go:63`

- [ ] **Step 1: Add WithMouseCellMotion to tea.NewProgram**

```go
// cmd/ferry/main.go:63 — change:
p := tea.NewProgram(app.NewWithOptions(opts), tea.WithAltScreen())
// to:
p := tea.NewProgram(app.NewWithOptions(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
```

- [ ] **Step 2: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 3: Commit**

```bash
git add cmd/ferry/main.go
git commit -m "feat: enable mouse cell motion tracking"
```

---

### Task 2: Route mouse events in app.go Update

The app's `Update` method needs to intercept `tea.MouseMsg` and route it to the correct component based on the current state and the mouse coordinates.

**Files:**
- Modify: `internal/app/app.go:221-413`

- [ ] **Step 1: Add mouse coordinate helper methods**

Add these methods to Model. They use the same layout math as `setPaneSizes()` and `viewBrowser()` to determine which region was clicked.

```go
// mouseRegion determines which UI region a mouse event falls in during browser state.
// Returns: "left-pane", "right-pane", "info-panel", "status-bar", or "".
func (m Model) mouseRegion(x, y int) string {
	paneWidth := m.width / 2
	paneHeight := m.height - m.statusBar.Height() - m.infoPanel.Height()

	switch {
	case y < paneHeight && x < paneWidth:
		return "left-pane"
	case y < paneHeight && x >= paneWidth:
		return "right-pane"
	case m.infoPanel.IsVisible() && y >= paneHeight && y < paneHeight+m.infoPanel.Height():
		return "info-panel"
	case y >= m.height-m.statusBar.Height():
		return "status-bar"
	default:
		return ""
	}
}

// paneLocalY converts a terminal Y coordinate to a Y coordinate local to the pane's content area.
// Returns the row index within the pane (0 = first row inside border).
func (m Model) paneLocalY(y int) int {
	// Pane border is 1 row at top, so content starts at y=1 within the pane.
	return y - 1
}

// paneLocalX converts a terminal X coordinate to a local X for the right pane.
func (m Model) rightPaneLocalX(x int) int {
	return x - m.width/2
}
```

- [ ] **Step 2: Handle tea.MouseMsg in the top-level Update switch**

Add a `tea.MouseMsg` case in the main Update method (after the `tea.KeyMsg` case around line 228). This goes before the state-specific switch.

```go
case tea.MouseMsg:
	switch m.state {
	case statePicker:
		return m.updatePickerMouse(msg)
	case stateBrowser:
		return m.updateBrowserMouse(msg)
	case stateSync:
		return m.updateSyncMouse(msg)
	}
	return m, nil
```

- [ ] **Step 3: Add stub mouse handler for browser state**

```go
func (m Model) updateBrowserMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Dismiss help overlay on any click.
	if m.helpOverlay.IsVisible() {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.helpOverlay.SetVisible(false)
		}
		return m, nil
	}

	region := m.mouseRegion(msg.X, msg.Y)

	switch region {
	case "left-pane":
		if m.activePane != 0 {
			m.activePane = 0
			m.localPane.SetActive(true)
			m.remotePane.SetActive(false)
			m.updateStatusSelection()
		}
		var cmd tea.Cmd
		m.localPane, cmd = m.localPane.Update(msg)
		m.updateStatusSelection()
		return m, cmd

	case "right-pane":
		if m.activePane != 1 {
			m.activePane = 1
			m.localPane.SetActive(false)
			m.remotePane.SetActive(true)
			m.updateStatusSelection()
		}
		// Adjust X coordinate to be pane-local for right pane.
		msg.X = m.rightPaneLocalX(msg.X)
		var cmd tea.Cmd
		m.remotePane, cmd = m.remotePane.Update(msg)
		m.updateStatusSelection()
		return m, cmd

	case "status-bar":
		return m.handleStatusBarClick(msg)

	case "info-panel":
		// Click on info panel — toggle it off.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.infoPanel.SetVisible(false)
			m.setPaneSizes()
		}
		return m, nil
	}

	return m, nil
}
```

- [ ] **Step 4: Add stub mouse handlers for picker and sync**

```go
func (m Model) updatePickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m Model) updateSyncMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.diffView, cmd = m.diffView.Update(msg)
	return m, cmd
}
```

- [ ] **Step 5: Add status bar click handler**

```go
func (m Model) handleStatusBarClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	statusY := m.height - m.statusBar.Height()
	localY := msg.Y - statusY

	if localY == 0 {
		// Clicked the connection/info line — toggle info panel.
		m.infoPanel.Toggle()
		if m.infoPanel.IsVisible() {
			if m.activePane == 0 {
				m.infoPanel.SetEntry(m.localPane.CurrentEntry())
			} else {
				m.infoPanel.SetEntry(m.remotePane.CurrentEntry())
			}
		}
		m.setPaneSizes()
	}
	// Click on hint line (localY == 1) — no action needed.

	return m, nil
}
```

- [ ] **Step 6: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 7: Commit**

```bash
git add internal/app/app.go
git commit -m "feat: route mouse events to UI components by region"
```

---

## Chunk 2: Pane Mouse Interaction

### Task 3: Add mouse handling to pane.go

This is the core interaction — clicking files, scrolling, double-clicking, and drag-selecting in the file browser panes.

**Files:**
- Modify: `internal/ui/pane/pane.go:70-103` (Model struct), `209-275` (Update method)

- [ ] **Step 1: Add mouse tracking fields to Model**

Add to the Model struct (after `lastKey` field, around line 88):

```go
	lastClickTime time.Time  // for double-click detection
	lastClickRow  int        // row of last click for double-click
	dragStart     int        // starting cursor position for drag-select (-1 = not dragging)
```

- [ ] **Step 2: Add a MouseMsg case to Update**

In the `Update` method (around line 209), add a case for `tea.MouseMsg` alongside the existing `tea.KeyMsg` case:

```go
	case tea.MouseMsg:
		if m.find {
			return m.updateFindMouse(msg)
		}
		return m.updateMouse(msg)
```

- [ ] **Step 3: Implement updateMouse**

This maps mouse Y to file list rows, handles click/scroll/double-click/drag.

```go
func (m Model) updateMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
			return m, nil

		case tea.MouseButtonWheelDown:
			if m.cursor < m.visibleCount()-1 {
				m.cursor++
				m.ensureVisible()
			}
			return m, nil

		case tea.MouseButtonLeft:
			row := m.mouseToRow(msg.Y)
			if row < 0 {
				return m, nil
			}

			now := time.Now()
			isDoubleClick := row == m.lastClickRow && now.Sub(m.lastClickTime) < 400*time.Millisecond
			m.lastClickTime = now
			m.lastClickRow = row

			if isDoubleClick {
				// Double-click: enter dir or transfer file.
				m.cursor = row
				m.ensureVisible()
				if e := m.CurrentEntry(); e != nil {
					if e.IsDir {
						target := e.Path
						m.selected = make(map[string]bool)
						m.anchor = -1
						return m, m.listDir(target)
					}
					return m, func() tea.Msg {
						return TransferRequestMsg{Entries: []fs.Entry{*e}}
					}
				}
				return m, nil
			}

			// Single click: move cursor.
			m.cursor = row
			m.dragStart = row
			m.ensureVisible()
			return m, nil

		case tea.MouseButtonRight:
			row := m.mouseToRow(msg.Y)
			if row < 0 {
				return m, nil
			}
			m.cursor = row
			m.ensureVisible()
			// Right-click: emit a context menu request.
			return m, func() tea.Msg {
				return ContextMenuMsg{X: msg.X, Y: msg.Y}
			}
		}

	case tea.MouseActionMotion:
		// Drag-select: select range from dragStart to current row.
		if m.dragStart >= 0 {
			row := m.mouseToRow(msg.Y)
			if row < 0 {
				return m, nil
			}
			// Select everything between dragStart and current row.
			lo, hi := m.dragStart, row
			if lo > hi {
				lo, hi = hi, lo
			}
			m.selected = make(map[string]bool)
			for i := lo; i <= hi; i++ {
				idx := m.mapIndex(i)
				if idx >= 0 && idx < len(m.entries) {
					m.selected[m.entries[idx].Path] = true
				}
			}
			m.cursor = row
			m.ensureVisible()
			return m, nil
		}

	case tea.MouseActionRelease:
		m.dragStart = -1
	}

	return m, nil
}
```

- [ ] **Step 4: Add mouseToRow helper**

This converts a terminal Y coordinate (pane-local, since the app adjusts coordinates before forwarding) to a visible row index in the file list.

```go
// mouseToRow converts a mouse Y coordinate to a visible file list row index.
// Returns -1 if the click is outside the file list area.
func (m Model) mouseToRow(y int) int {
	// Pane layout: border(1) + header(1) + file rows + footer(1) + border(1)
	// File list starts at y=2 (border + header), each row is 1 line.
	listStart := 2 // border top + header
	row := y - listStart + m.offset
	if row < 0 || row >= m.visibleCount() {
		return -1
	}
	return row
}
```

- [ ] **Step 5: Add ContextMenuMsg type**

```go
// ContextMenuMsg is emitted on right-click to request a context menu.
type ContextMenuMsg struct {
	X, Y int // terminal coordinates for positioning the menu
}
```

- [ ] **Step 6: Implement updateFindMouse for find mode**

```go
func (m Model) updateFindMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
		if m.findCursor > 0 {
			m.findCursor--
			m.ensureFindVisible()
		}
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
		if m.findCursor < len(m.findFiltered)-1 {
			m.findCursor++
			m.ensureFindVisible()
		}
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		// Click on a find result row.
		listStart := 2
		row := msg.Y - listStart + m.findOffset
		if row >= 0 && row < len(m.findFiltered) {
			m.findCursor = row
			m.ensureFindVisible()
		}
	}
	return m, nil
}
```

- [ ] **Step 7: Initialize new fields in New()**

Update the `New` function (line 106) to initialize `dragStart`:

```go
// In New(), add to the returned Model:
dragStart: -1,
```

- [ ] **Step 8: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 9: Manual test**

Run ferry with the test container, verify:
1. Scroll wheel moves cursor up/down in file list
2. Left click moves cursor to clicked file
3. Double-click enters directories
4. Double-click on file transfers it
5. Click+drag selects a range of files
6. Click on inactive pane switches focus

- [ ] **Step 10: Commit**

```bash
git add internal/ui/pane/pane.go
git commit -m "feat: add mouse click, scroll, double-click, and drag-select to file panes"
```

---

## Chunk 3: Picker & Sync View Mouse Support

### Task 4: Add mouse handling to picker.go

**Files:**
- Modify: `internal/ui/picker/picker.go:79-142`

- [ ] **Step 1: Add tea.MouseMsg case to picker Update**

Add to the `Update` switch (after the `tea.KeyMsg` case, around line 101):

```go
	case tea.MouseMsg:
		switch {
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
			if m.cursor < m.totalItems()-1 {
				m.cursor++
			}
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
			// Map click Y to a list item.
			// View layout: logo(~2 lines) + tagline + blank + search input + blank = ~6 lines before list
			row := m.mouseToItem(msg.Y)
			if row >= 0 && row < m.totalItems() {
				m.cursor = row
				// Double-click detection: if already on this item, treat as enter.
			}
		}
		return m, cmd
```

Note: We need to handle this more carefully. The picker view has a variable header height. Let me adjust.

- [ ] **Step 2: Add mouse helper methods to picker**

```go
// lastClickTime and lastClickItem track double-click state.
// Add to Model struct:
//   lastClickTime time.Time
//   lastClickItem int

func (m *Model) mouseToItem(y int) int {
	// The list starts after: logo lines (~2) + tagline(1) + blank(1) + search(1) + blank(1) = 6 rows.
	// But logo height varies. Use a fixed estimate of 6 rows before the list.
	listStartY := 6
	if y < listStartY {
		return -1
	}

	// Compute scroll offset (same logic as View).
	maxVisible := m.height - 14
	if maxVisible < 3 {
		maxVisible = 3
	}

	// Find display-line offset for cursor to compute scroll start.
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}

	displayRow := y - listStartY + start

	// Map display row to selectable item index.
	// We need to account for non-selectable divider lines in S3 section.
	current := 0
	selectableIdx := -1
	for i := 0; i < len(m.filtered); i++ {
		if current == displayRow {
			selectableIdx = i
			break
		}
		current++
	}
	if selectableIdx < 0 && len(m.filteredBuckets) > 0 {
		// Check S3 section — has divider lines.
		lastProfile := ""
		for i, b := range m.filteredBuckets {
			profile := b.Profile
			if profile == "" {
				profile = "default"
			}
			if profile != lastProfile {
				current++ // divider line
				lastProfile = profile
			}
			if current == displayRow {
				selectableIdx = len(m.filtered) + i
				break
			}
			current++
		}
	}
	return selectableIdx
}
```

Actually, this mapping is fragile because the picker view constructs display lines dynamically with S3 dividers. A simpler approach: add `lastClickTime`/`lastClickItem` fields and handle double-click to connect.

- [ ] **Step 1 (revised): Add mouse fields to picker Model**

```go
// Add to Model struct (picker.go:34-44):
lastClickTime time.Time
lastClickItem int
```

- [ ] **Step 2 (revised): Add mouse handling in picker Update**

Insert before the final `return m, cmd` at line 141:

```go
	case tea.MouseMsg:
		switch {
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
			if m.cursor < m.totalItems()-1 {
				m.cursor++
			}
			return m, nil
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
			item := m.mouseToItem(msg.Y)
			if item >= 0 && item < m.totalItems() {
				now := time.Now()
				if item == m.lastClickItem && now.Sub(m.lastClickTime) < 400*time.Millisecond {
					// Double-click: connect.
					m.cursor = item
					return m, m.handleEnter()
				}
				m.cursor = item
				m.lastClickTime = now
				m.lastClickItem = item
			}
			return m, nil
		}
```

- [ ] **Step 3 (revised): Implement mouseToItem for picker**

The picker View builds a `lines` slice with selectable and non-selectable entries. We replicate that logic to map Y to item index.

```go
func (m Model) mouseToItem(y int) int {
	// Header: logo + tagline = ~3 lines, then blank + search + blank = 3 more.
	// Total header before list: 6 lines.
	listStartY := 6
	if y < listStartY {
		return -1
	}

	maxVisible := m.height - 14
	if maxVisible < 3 {
		maxVisible = 3
	}

	// Reconstruct which display lines are visible (same scroll logic as View).
	type displayLine struct {
		selectable bool
		index      int
	}
	var lines []displayLine
	for i := range m.filtered {
		lines = append(lines, displayLine{selectable: true, index: i})
	}
	if len(m.filteredBuckets) > 0 {
		lastProfile := ""
		for i, b := range m.filteredBuckets {
			profile := b.Profile
			if profile == "" {
				profile = "default"
			}
			if profile != lastProfile {
				lines = append(lines, displayLine{selectable: false, index: -1})
				lastProfile = profile
			}
			lines = append(lines, displayLine{selectable: true, index: len(m.filtered) + i})
		}
	}

	// Find scroll start (mirrors View logic).
	cursorDisplayIdx := 0
	for i, l := range lines {
		if l.selectable && l.index == m.cursor {
			cursorDisplayIdx = i
			break
		}
	}
	start := 0
	if cursorDisplayIdx >= maxVisible {
		start = cursorDisplayIdx - maxVisible + 1
	}

	clickedDisplayIdx := start + (y - listStartY)
	if clickedDisplayIdx < 0 || clickedDisplayIdx >= len(lines) {
		return -1
	}
	l := lines[clickedDisplayIdx]
	if !l.selectable {
		return -1
	}
	return l.index
}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 5: Commit**

```bash
git add internal/ui/picker/picker.go
git commit -m "feat: add mouse click, scroll, and double-click to connection picker"
```

---

### Task 5: Add mouse handling to diff.go (sync view)

**Files:**
- Modify: `internal/ui/diff/diff.go:55-71` (Model), `188-268` (Update)

- [ ] **Step 1: Add mouse fields to diff Model**

```go
// Add to Model struct:
lastClickTime time.Time
lastClickRow  int
```

- [ ] **Step 2: Add tea.MouseMsg case to diff Update**

In the `Update` method, add before or after the `tea.KeyMsg` case:

```go
	case tea.MouseMsg:
		if m.syncing || m.comparing {
			return m, nil
		}
		return m.updateMouse(msg)
```

- [ ] **Step 3: Implement updateMouse for diff**

```go
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
		}
	}

	return m, nil
}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 5: Commit**

```bash
git add internal/ui/diff/diff.go
git commit -m "feat: add mouse click, scroll, and double-click toggle to sync view"
```

---

## Chunk 4: Context Menu

### Task 6: Create the context menu component

A floating menu that appears at the mouse cursor position. It renders a bordered box with selectable action items. Left-click an item to execute, click outside or Esc to dismiss.

**Files:**
- Create: `internal/ui/contextmenu/menu.go`

- [ ] **Step 1: Create the context menu package**

```go
// internal/ui/contextmenu/menu.go
package contextmenu

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	menuWidth := m.menuWidth()
	menuHeight := len(items) + 2 // items + border
	if x+menuWidth > m.width {
		x = m.width - menuWidth
	}
	if y+menuHeight > m.height {
		y = m.height - menuHeight
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
			// Check if click is inside the menu.
			menuW := m.menuWidth()
			menuH := len(m.items) + 2
			if msg.X >= m.x && msg.X < m.x+menuW && msg.Y >= m.y && msg.Y < m.y+menuH {
				// Map click to menu item.
				row := msg.Y - m.y - 1 // -1 for top border
				if row >= 0 && row < len(m.items) {
					m.visible = false
					action := m.items[row].Action
					return m, func() tea.Msg { return SelectMsg{Action: action} }
				}
			} else {
				// Click outside: dismiss.
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

// View renders the context menu as a floating box.
func (m Model) View() string {
	if !m.visible || len(m.items) == 0 {
		return ""
	}

	var rows []string
	itemWidth := m.menuWidth() - 4 // inner width
	for i, item := range m.items {
		label := item.Label
		pad := itemWidth - len(label)
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
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Render(content)

	// Position the box at (m.x, m.y) using lipgloss.Place with alignment offsets.
	return lipgloss.Place(m.width, m.height,
		lipgloss.Left, lipgloss.Top,
		box,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// PlacedView renders the menu at its position over existing content.
// Use this when you need to overlay the menu on top of other content.
func (m Model) PlacedView(baseWidth, baseHeight int) string {
	if !m.visible || len(m.items) == 0 {
		return ""
	}

	var rows []string
	itemWidth := m.menuWidth() - 4
	for i, item := range m.items {
		label := item.Label
		pad := itemWidth - len(label)
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
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Render(content)
}
```

- [ ] **Step 2: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 3: Commit**

```bash
git add internal/ui/contextmenu/menu.go
git commit -m "feat: add context menu component for right-click menus"
```

---

### Task 7: Wire context menu into app.go

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Add context menu to Model struct**

```go
// Add import:
"github.com/HopperShell/ferry/internal/ui/contextmenu"

// Add to Model struct:
contextMenu contextmenu.Model
```

- [ ] **Step 2: Initialize context menu in NewWithOptions**

```go
// In the Model initialization (around line 187):
contextMenu: contextmenu.New(),
```

- [ ] **Step 3: Update setPaneSizes to set context menu size**

```go
// In setPaneSizes, add:
m.contextMenu.SetSize(m.width, m.height)
```

- [ ] **Step 4: Handle ContextMenuMsg from pane**

Add to the `updateBrowser` method (in the message switch):

```go
	case pane.ContextMenuMsg:
		items := []contextmenu.Item{
			{Label: "Copy      yy", Action: "copy"},
			{Label: "Move       m", Action: "move"},
			{Label: "Delete    dd", Action: "delete"},
			{Label: "Rename     r", Action: "rename"},
			{Label: "New Folder D", Action: "mkdir"},
			{Label: "Edit       e", Action: "edit"},
			{Label: "Sync       S", Action: "sync"},
			{Label: "Mirror     M", Action: "mirror"},
			{Label: "Info       i", Action: "info"},
		}
		m.contextMenu.Show(msg.X, msg.Y, items)
		return m, nil
```

- [ ] **Step 5: Route input to context menu when visible**

At the top of `updateBrowser`, before the inputMode check:

```go
	// Context menu intercepts all input when visible.
	if m.contextMenu.IsVisible() {
		var cmd tea.Cmd
		m.contextMenu, cmd = m.contextMenu.Update(msg)
		return m, cmd
	}
```

- [ ] **Step 6: Handle context menu SelectMsg**

Add to `updateBrowser` message switch:

```go
	case contextmenu.SelectMsg:
		switch msg.Action {
		case "copy":
			return m.doYank(false)
		case "move":
			return m.doYank(true)
		case "delete":
			return m.startDelete()
		case "rename":
			return m.startRename()
		case "mkdir":
			return m.startMkdir()
		case "edit":
			return m.startEdit()
		case "sync":
			return m.startSync()
		case "mirror":
			// Mirror needs direction — just start mirror pending state.
			m.state = stateSync
			// Trigger sync first, then user chooses direction.
			return m.startSync()
		case "info":
			m.infoPanel.Toggle()
			if m.infoPanel.IsVisible() {
				if m.activePane == 0 {
					m.infoPanel.SetEntry(m.localPane.CurrentEntry())
				} else {
					m.infoPanel.SetEntry(m.remotePane.CurrentEntry())
				}
			}
			m.setPaneSizes()
			return m, nil
		}

	case contextmenu.DismissMsg:
		return m, nil
```

- [ ] **Step 7: Render context menu overlay in viewBrowser**

In `viewBrowser()`, after the base layout is assembled but before returning, overlay the context menu:

```go
	// Overlay context menu if visible.
	if m.contextMenu.IsVisible() {
		// The context menu renders itself positioned via lipgloss.Place.
		return m.contextMenu.View()
	}
```

Wait — that would replace the entire view. Instead, we should overlay it. Since Bubble Tea doesn't have true z-ordering, the simplest approach is to render the context menu as a full-screen overlay (like the help overlay does) — it captures all input anyway.

```go
	// In viewBrowser(), before the help overlay check:
	if m.contextMenu.IsVisible() {
		return m.contextMenu.View()
	}
```

- [ ] **Step 8: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 9: Manual test**

Right-click on a file → context menu appears. Click an action → executes. Click outside → dismisses. Esc → dismisses.

- [ ] **Step 10: Commit**

```bash
git add internal/app/app.go internal/ui/contextmenu/menu.go
git commit -m "feat: wire up right-click context menu in file browser"
```

---

## Chunk 5: Sync View Context Menu & Help/Modal Dismiss

### Task 8: Add right-click context menu to sync view

**Files:**
- Modify: `internal/ui/diff/diff.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Add ContextMenuMsg to diff package**

```go
// In diff.go, add:
type ContextMenuMsg struct {
	X, Y int
}
```

- [ ] **Step 2: Emit context menu on right-click in diff updateMouse**

In the `tea.MouseButtonRight` case of `updateMouse` (diff.go, to be added):

```go
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
```

- [ ] **Step 3: Handle diff ContextMenuMsg in app.go**

In `updateSync` (or `updateSyncMouse`), handle the diff context menu:

```go
	case diff.ContextMenuMsg:
		items := []contextmenu.Item{
			{Label: "Push →     l", Action: "push"},
			{Label: "Pull ←     h", Action: "pull"},
			{Label: "Select All a", Action: "select-all"},
			{Label: "Deselect     ", Action: "deselect"},
			{Label: "Mirror Push  ", Action: "mirror-push"},
			{Label: "Mirror Pull  ", Action: "mirror-pull"},
		}
		m.contextMenu.Show(msg.X, msg.Y, items)
		return m, nil
```

- [ ] **Step 4: Handle sync context menu SelectMsg in updateSync**

```go
	case contextmenu.SelectMsg:
		switch msg.Action {
		case "push":
			// Same as pressing right/l.
			sel := m.diffView.SelectedEntries()
			if len(sel) > 0 {
				return m, func() tea.Msg {
					return diff.SyncAction{Entries: sel, Direction: "push"}
				}
			}
		case "pull":
			sel := m.diffView.SelectedEntries()
			if len(sel) > 0 {
				return m, func() tea.Msg {
					return diff.SyncAction{Entries: sel, Direction: "pull"}
				}
			}
		case "select-all":
			// Select all visible entries.
			visible := m.diffView.DiffEntries()
			for i, e := range visible {
				if e.Status != transfer.DiffSame {
					m.diffView.Select(i)
				}
			}
		case "deselect":
			m.diffView.ClearSelection()
		case "mirror-push":
			return m, func() tea.Msg {
				return diff.MirrorAction{Direction: "push"}
			}
		case "mirror-pull":
			return m, func() tea.Msg {
				return diff.MirrorAction{Direction: "pull"}
			}
		}
```

- [ ] **Step 5: Add Select/ClearSelection helper methods to diff Model**

```go
func (m *Model) Select(idx int) {
	m.selected[idx] = true
}

func (m *Model) ClearSelection() {
	m.selected = make(map[int]bool)
}
```

- [ ] **Step 6: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 7: Commit**

```bash
git add internal/ui/diff/diff.go internal/app/app.go
git commit -m "feat: add right-click context menu to sync view"
```

---

### Task 9: Help overlay and modal dismiss on click

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Help overlay click-to-dismiss**

Already handled in Task 2's `updateBrowserMouse` — any left click dismisses the help overlay. Verify this works.

- [ ] **Step 2: Error message click-to-dismiss in status bar**

Already handled in Task 2's `handleStatusBarClick`. The first line click toggles info panel. Add error dismissal for when an error is showing:

In `handleStatusBarClick`, add after the `localY == 0` block:

```go
	// If there's an error showing and user clicks anywhere on the status bar, clear it.
	if m.statusBar.HasError() {
		m.statusBar.SetError("")
	}
```

- [ ] **Step 3: Add HasError method to StatusBar**

```go
// In statusbar.go:
func (s *StatusBar) HasError() bool {
	return s.error != ""
}
```

- [ ] **Step 4: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/ui/statusbar/statusbar.go
git commit -m "feat: click to dismiss help overlay and error messages"
```

---

## Chunk 6: Final Integration & Testing

### Task 10: Update help overlay to show mouse hints

**Files:**
- Modify: `internal/ui/modal/help.go`

- [ ] **Step 1: Add mouse section to help overlay**

In the right column (after the Sync View section, around line 101):

```go
		"",
		headerStyle.Render("Mouse"),
		entry("Click", "Select / focus"),
		entry("Dbl-click", "Open / transfer"),
		entry("Right-click", "Context menu"),
		entry("Scroll", "Navigate list"),
		entry("Drag", "Range select"),
```

- [ ] **Step 2: Build and verify**

Run: `go build ./...`
Expected: compiles clean

- [ ] **Step 3: Commit**

```bash
git add internal/ui/modal/help.go
git commit -m "feat: add mouse hints to help overlay"
```

---

### Task 11: End-to-end manual test

- [ ] **Step 1: Start test container**

```bash
docker compose -f test/docker/docker-compose.yml up -d
```

- [ ] **Step 2: Run ferry and test all mouse interactions**

Test checklist:
- [ ] **Picker:** Scroll through hosts, click to select, double-click to connect
- [ ] **Browser - Left pane:** Click files, scroll, double-click dirs, double-click files to transfer
- [ ] **Browser - Right pane:** Same as left pane but on remote
- [ ] **Browser - Focus switch:** Click on inactive pane switches focus
- [ ] **Browser - Drag select:** Click+drag to select range of files
- [ ] **Browser - Right click:** Context menu appears, items work (copy, delete, rename, etc.)
- [ ] **Browser - Context menu dismiss:** Click outside dismisses, Esc dismisses
- [ ] **Browser - Status bar:** Click connection info toggles info panel
- [ ] **Browser - Status bar:** Click dismisses error message
- [ ] **Browser - Help overlay:** Click anywhere dismisses
- [ ] **Browser - Info panel:** Click dismisses
- [ ] **Sync view:** Click entries, scroll, double-click to toggle selection
- [ ] **Sync view - Right click:** Context menu with push/pull/mirror/select actions
- [ ] **Find mode:** Scroll through results, click to select
- [ ] **Help overlay:** Shows mouse hints section

- [ ] **Step 3: Run existing tests**

```bash
FERRY_INTEGRATION=1 go test -v -count=1 ./...
```
Expected: all tests pass (mouse support doesn't affect test behavior)

- [ ] **Step 4: Final commit and tag**

```bash
git add -A
git commit -m "feat: full mouse support — click, scroll, double-click, drag-select, context menus"
git tag v0.2.0
```
