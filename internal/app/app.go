// internal/app/app.go
package app

import (
	"fmt"
	"path/filepath"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andrewstuart/ferry/internal/fs"
	ferrySSH "github.com/andrewstuart/ferry/internal/ssh"
	"github.com/andrewstuart/ferry/internal/transfer"
	"github.com/andrewstuart/ferry/internal/ui/pane"
	"github.com/andrewstuart/ferry/internal/ui/picker"
	"github.com/andrewstuart/ferry/internal/ui/statusbar"
	"github.com/andrewstuart/ferry/internal/ui/theme"
)

// appState represents the current UI state.
type appState int

const (
	statePicker     appState = iota // show connection picker
	stateConnecting                 // show spinner while connecting
	stateBrowser                    // dual-pane file browser
)

// Messages for async connect results.
type connectSuccessMsg struct {
	conn *ferrySSH.Connection
}

type connectErrorMsg struct {
	err error
}

// progressMsg wraps a transfer progress event as a Bubble Tea message.
type progressMsg transfer.ProgressEvent

// transferDoneMsg signals that all transfers have completed.
type transferDoneMsg struct{}

// clipboard stores yanked/cut entries for paste operations.
type clipboard struct {
	entries []fs.Entry
	srcFS   fs.FileSystem
	srcPath string
	cut     bool // true for move (m), false for copy (yy)
}

// Model is the top-level Bubble Tea model for ferry.
type Model struct {
	state  appState
	width  int
	height int

	// Picker
	picker picker.Model

	// Connecting
	connectHost string
	spinner     spinner.Model

	// Connection
	conn *ferrySSH.Connection

	// Browser
	localPane  pane.Model
	remotePane pane.Model
	activePane int // 0 = left (local), 1 = right (remote)
	statusBar  statusbar.StatusBar
	lastKey    string // for yy/dd detection at app level

	// File operations
	clip       *clipboard
	engine     *transfer.Engine
	inputMode  string // "", "rename", "mkdir", "confirm-delete"
	inputField textinput.Model

	// Error
	err error
}

// New creates the initial app model.
func New() Model {
	hosts, _ := ferrySSH.ParseConfigHosts(ferrySSH.DefaultConfigPath())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Cyan)

	ti := textinput.New()
	ti.CharLimit = 256

	return Model{
		state:      statePicker,
		picker:     picker.New(hosts),
		spinner:    sp,
		inputField: ti,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.picker.Init(), m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.handleResize()

	case tea.KeyMsg:
		// Global quit keys.
		switch msg.String() {
		case "ctrl+c":
			if m.engine != nil {
				m.engine.Cancel()
			}
			if m.conn != nil {
				m.conn.Close()
			}
			return m, tea.Quit
		case "q":
			if m.state == stateBrowser && m.inputMode == "" {
				if m.engine != nil {
					m.engine.Cancel()
				}
				if m.conn != nil {
					m.conn.Close()
				}
				return m, tea.Quit
			}
		}

	case progressMsg:
		evt := transfer.ProgressEvent(msg)
		if evt.Done && evt.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Transfer failed: %s: %v", evt.Name, evt.Err))
		}
		// Check if all transfers are done.
		if m.engine != nil && m.engine.ActiveCount() == 0 {
			// If this was a move (cut), delete sources.
			if m.clip != nil && m.clip.cut {
				for _, entry := range m.clip.entries {
					_ = m.clip.srcFS.Remove(entry.Path)
				}
				m.clip = nil
			}
			return m, tea.Batch(
				m.localPane.Refresh(),
				m.remotePane.Refresh(),
				listenForProgress(m.engine.Progress()),
			)
		}
		return m, listenForProgress(m.engine.Progress())
	}

	switch m.state {
	case statePicker:
		return m.updatePicker(msg)
	case stateConnecting:
		return m.updateConnecting(msg)
	case stateBrowser:
		return m.updateBrowser(msg)
	}

	return m, nil
}

func (m Model) View() string {
	switch m.state {
	case statePicker:
		return m.picker.View()
	case stateConnecting:
		return m.viewConnecting()
	case stateBrowser:
		return m.viewBrowser()
	}
	return ""
}

// --- State: Picker ---

func (m Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case picker.HostSelected:
		m.state = stateConnecting
		m.connectHost = msg.Host
		m.err = nil
		return m, tea.Batch(m.spinner.Tick, m.doConnect(msg.Host))
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

// --- State: Connecting ---

func (m Model) updateConnecting(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectSuccessMsg:
		m.conn = msg.conn
		m.state = stateBrowser

		// Create panes.
		localFS := fs.NewLocalFS()
		remoteFS, err := fs.NewRemoteFS(m.conn.Client)
		if err != nil {
			m.state = statePicker
			m.err = fmt.Errorf("sftp: %w", err)
			return m, nil
		}

		m.localPane = pane.New(localFS, "Local")
		m.remotePane = pane.New(remoteFS, "Remote")
		m.activePane = 0
		m.localPane.SetActive(true)
		m.remotePane.SetActive(false)

		// Set status bar connection info.
		cfg := m.conn.Config
		connInfo := fmt.Sprintf("%s@%s:%s", cfg.User, cfg.HostName, cfg.Port)
		m.statusBar.SetConnection(connInfo)

		// Apply sizes.
		m.setPaneSizes()

		// Init both panes.
		return m, tea.Batch(m.localPane.Init(), m.remotePane.Init())

	case connectErrorMsg:
		m.state = statePicker
		m.err = msg.err
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) viewConnecting() string {
	msg := fmt.Sprintf("\n\n   %s Connecting to %s...\n", m.spinner.View(), m.connectHost)
	return lipgloss.NewStyle().Foreground(theme.White).Render(msg)
}

// --- State: Browser ---

func (m Model) updateBrowser(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle input modes first (rename, mkdir, confirm-delete).
	if m.inputMode != "" {
		return m.updateInputMode(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		defer func() { m.lastKey = key }()

		switch key {
		case "tab":
			m.activePane = 1 - m.activePane
			m.localPane.SetActive(m.activePane == 0)
			m.remotePane.SetActive(m.activePane == 1)
			m.updateStatusSelection()
			return m, nil

		case "y":
			if m.lastKey == "y" {
				// yy: yank/copy
				m.lastKey = ""
				return m.doYank(false)
			}
			return m, nil

		case "p":
			// paste
			return m.doPaste()

		case "d":
			if m.lastKey == "d" {
				// dd: delete with confirmation
				m.lastKey = ""
				return m.startDelete()
			}
			return m, nil

		case "r":
			// rename
			return m.startRename()

		case "m":
			// move: yank as cut, then paste
			return m.doYank(true)

		case "D":
			// mkdir
			return m.startMkdir()
		}
	}

	// Route message to active pane.
	var cmd tea.Cmd
	if m.activePane == 0 {
		m.localPane, cmd = m.localPane.Update(msg)
	} else {
		m.remotePane, cmd = m.remotePane.Update(msg)
	}
	m.updateStatusSelection()
	return m, cmd
}

func (m Model) updateInputMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.inputMode = ""
			m.inputField.Blur()
			return m, nil

		case "enter":
			return m.submitInput()

		case "n":
			if m.inputMode == "confirm-delete" {
				m.inputMode = ""
				m.inputField.Blur()
				return m, nil
			}
			// Fall through for other input modes.
		}

		if m.inputMode == "confirm-delete" {
			if msg.String() == "y" {
				return m.executeDelete()
			}
			return m, nil
		}
	}

	// Update the text input for rename/mkdir.
	var cmd tea.Cmd
	m.inputField, cmd = m.inputField.Update(msg)
	return m, cmd
}

func (m Model) submitInput() (tea.Model, tea.Cmd) {
	value := m.inputField.Value()
	mode := m.inputMode
	m.inputMode = ""
	m.inputField.Blur()

	_, activeFS, activePath := m.activePaneInfo()

	switch mode {
	case "rename":
		var entry *fs.Entry
		if m.activePane == 0 {
			entry = m.localPane.CurrentEntry()
		} else {
			entry = m.remotePane.CurrentEntry()
		}
		if entry != nil && value != "" && value != entry.Name {
			newPath := filepath.Join(filepath.Dir(entry.Path), value)
			if err := activeFS.Rename(entry.Path, newPath); err != nil {
				m.statusBar.SetError(fmt.Sprintf("Rename error: %v", err))
			} else {
				m.statusBar.SetError("")
			}
		}

	case "mkdir":
		if value != "" {
			dirPath := filepath.Join(activePath, value)
			if err := activeFS.Mkdir(dirPath, 0o755); err != nil {
				m.statusBar.SetError(fmt.Sprintf("Mkdir error: %v", err))
			} else {
				m.statusBar.SetError("")
			}
		}
	}

	// Refresh the active pane.
	if m.activePane == 0 {
		return m, m.localPane.Refresh()
	}
	return m, m.remotePane.Refresh()
}

// doYank stores selected entries in the clipboard.
func (m Model) doYank(cut bool) (tea.Model, tea.Cmd) {
	var entries []fs.Entry
	var srcFS fs.FileSystem
	var srcPath string

	if m.activePane == 0 {
		entries = m.localPane.SelectedEntries()
		srcFS = m.localPane.FS()
		srcPath = m.localPane.Path()
	} else {
		entries = m.remotePane.SelectedEntries()
		srcFS = m.remotePane.FS()
		srcPath = m.remotePane.Path()
	}

	if len(entries) == 0 {
		return m, nil
	}

	m.clip = &clipboard{
		entries: entries,
		srcFS:   srcFS,
		srcPath: srcPath,
		cut:     cut,
	}

	action := "Yanked"
	if cut {
		action = "Cut"
	}
	m.statusBar.SetError(fmt.Sprintf("%s %d item(s)", action, len(entries)))

	return m, nil
}

// doPaste creates transfer jobs from clipboard to the current pane's directory.
func (m Model) doPaste() (tea.Model, tea.Cmd) {
	if m.clip == nil || len(m.clip.entries) == 0 {
		m.statusBar.SetError("Nothing to paste")
		return m, nil
	}

	var dstFS fs.FileSystem
	var dstPath string
	if m.activePane == 0 {
		dstFS = m.localPane.FS()
		dstPath = m.localPane.Path()
	} else {
		dstFS = m.remotePane.FS()
		dstPath = m.remotePane.Path()
	}

	m.engine = transfer.NewEngine(2)

	for _, entry := range m.clip.entries {
		m.engine.EnqueueEntry(entry, m.clip.srcFS, m.clip.srcPath, dstFS, dstPath)
	}

	// Start engine in background.
	go m.engine.Start()

	m.statusBar.SetError(fmt.Sprintf("Transferring %d item(s)...", len(m.clip.entries)))

	return m, listenForProgress(m.engine.Progress())
}

// startDelete prompts for confirmation.
func (m Model) startDelete() (tea.Model, tea.Cmd) {
	var entries []fs.Entry
	if m.activePane == 0 {
		entries = m.localPane.SelectedEntries()
	} else {
		entries = m.remotePane.SelectedEntries()
	}
	if len(entries) == 0 {
		return m, nil
	}

	m.inputMode = "confirm-delete"
	m.statusBar.SetError(fmt.Sprintf("Delete %d item(s)? y/n", len(entries)))
	return m, nil
}

// executeDelete performs the actual deletion.
func (m Model) executeDelete() (tea.Model, tea.Cmd) {
	m.inputMode = ""

	var entries []fs.Entry
	var activeFS fs.FileSystem
	if m.activePane == 0 {
		entries = m.localPane.SelectedEntries()
		activeFS = m.localPane.FS()
	} else {
		entries = m.remotePane.SelectedEntries()
		activeFS = m.remotePane.FS()
	}

	var lastErr error
	for _, entry := range entries {
		if err := activeFS.Remove(entry.Path); err != nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		m.statusBar.SetError(fmt.Sprintf("Delete error: %v", lastErr))
	} else {
		m.statusBar.SetError("")
	}

	// Refresh the active pane.
	if m.activePane == 0 {
		return m, m.localPane.Refresh()
	}
	return m, m.remotePane.Refresh()
}

// startRename enters rename input mode.
func (m Model) startRename() (tea.Model, tea.Cmd) {
	var entry *fs.Entry
	if m.activePane == 0 {
		entry = m.localPane.CurrentEntry()
	} else {
		entry = m.remotePane.CurrentEntry()
	}
	if entry == nil {
		return m, nil
	}

	m.inputMode = "rename"
	m.inputField.SetValue(entry.Name)
	m.inputField.Focus()
	m.inputField.Prompt = "Rename: "
	m.inputField.CursorEnd()

	return m, textinput.Blink
}

// startMkdir enters mkdir input mode.
func (m Model) startMkdir() (tea.Model, tea.Cmd) {
	m.inputMode = "mkdir"
	m.inputField.SetValue("")
	m.inputField.Focus()
	m.inputField.Prompt = "New dir: "

	return m, textinput.Blink
}

func (m Model) viewBrowser() string {
	leftView := m.localPane.View()
	rightView := m.remotePane.View()

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)

	var bar string
	if m.inputMode == "rename" || m.inputMode == "mkdir" {
		bar = m.inputField.View()
	} else {
		bar = m.statusBar.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, panes, bar)
}

// --- Helpers ---

func (m *Model) handleResize() (tea.Model, tea.Cmd) {
	switch m.state {
	case statePicker:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		return m, cmd
	case stateBrowser:
		m.setPaneSizes()
	}
	return m, nil
}

func (m *Model) setPaneSizes() {
	paneWidth := (m.width) / 2
	paneHeight := m.height - 1 // reserve 1 row for status bar

	m.localPane.SetSize(paneWidth, paneHeight)
	m.remotePane.SetSize(m.width-paneWidth, paneHeight)
	m.statusBar.SetWidth(m.width)
}

func (m *Model) updateStatusSelection() {
	var count int
	if m.activePane == 0 {
		count = len(m.localPane.SelectedEntries())
	} else {
		count = len(m.remotePane.SelectedEntries())
	}
	// SelectedEntries returns cursor item if nothing selected, so 1 means "none explicitly selected".
	if count <= 1 {
		count = 0
	}
	m.statusBar.SetSelection(count)
}

// activePaneInfo returns the active pane, its FS, and current path.
func (m Model) activePaneInfo() (*pane.Model, fs.FileSystem, string) {
	if m.activePane == 0 {
		return &m.localPane, m.localPane.FS(), m.localPane.Path()
	}
	return &m.remotePane, m.remotePane.FS(), m.remotePane.Path()
}

func (m Model) doConnect(host string) tea.Cmd {
	return func() tea.Msg {
		conn, err := ferrySSH.Connect(ferrySSH.ConnectOptions{
			Host: host,
		})
		if err != nil {
			return connectErrorMsg{err: err}
		}
		return connectSuccessMsg{conn: conn}
	}
}

// listenForProgress returns a Cmd that reads the next progress event from the channel.
func listenForProgress(ch <-chan transfer.ProgressEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return progressMsg(event)
	}
}
