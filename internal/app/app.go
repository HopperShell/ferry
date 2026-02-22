// internal/app/app.go
package app

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andrewstuart/ferry/internal/editor"
	"github.com/andrewstuart/ferry/internal/fs"
	ferrySSH "github.com/andrewstuart/ferry/internal/ssh"
	"github.com/andrewstuart/ferry/internal/transfer"
	"github.com/andrewstuart/ferry/internal/ui/diff"
	"github.com/andrewstuart/ferry/internal/ui/modal"
	"github.com/andrewstuart/ferry/internal/ui/pane"
	"github.com/andrewstuart/ferry/internal/ui/picker"
	"github.com/andrewstuart/ferry/internal/ui/statusbar"
	"github.com/andrewstuart/ferry/internal/ui/theme"
	transferUI "github.com/andrewstuart/ferry/internal/ui/transfer"
)

// appState represents the current UI state.
type appState int

const (
	statePicker     appState = iota // show connection picker
	stateConnecting                 // show spinner while connecting
	stateBrowser                    // dual-pane file browser
	stateSync                       // sync/diff view
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

// clearErrorMsg is sent after a timeout to auto-dismiss the status bar error.
type clearErrorMsg struct{}

// reconnectMsg carries the result of a reconnect attempt.
type reconnectMsg struct {
	conn *ferrySSH.Connection
	err  error
}

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

	// Editor
	editSession *editor.EditSession

	// Transfer overlay
	overlay *transferUI.Overlay

	// Info panel and help overlay
	infoPanel   *modal.InfoPanel
	helpOverlay *modal.HelpOverlay

	// Sync/diff view
	diffView diff.Model

	// Error
	err error
}

// Options configures how the app starts.
type Options struct {
	Host string // If set, skip picker and connect directly
}

// New creates the initial app model with the connection picker.
func New() Model {
	return NewWithOptions(Options{})
}

// NewWithOptions creates the initial app model with the given options.
// If opts.Host is set, the picker is skipped and a direct connection is initiated.
func NewWithOptions(opts Options) Model {
	hosts, _ := ferrySSH.ParseConfigHosts(ferrySSH.DefaultConfigPath())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Cyan)

	ti := textinput.New()
	ti.CharLimit = 256

	m := Model{
		state:       statePicker,
		picker:      picker.New(hosts),
		spinner:     sp,
		inputField:  ti,
		overlay:     transferUI.NewOverlay(),
		infoPanel:   modal.NewInfoPanel(),
		helpOverlay: modal.NewHelpOverlay(),
		diffView:    diff.New(),
	}

	if opts.Host != "" {
		m.state = stateConnecting
		m.connectHost = opts.Host
	}

	return m
}

func (m Model) Init() tea.Cmd {
	if m.state == stateConnecting {
		return tea.Batch(m.spinner.Tick, m.doConnect(m.connectHost))
	}
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
			if (m.state == stateBrowser || m.state == stateSync) && m.inputMode == "" {
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
			if isConnectionError(evt.Err) {
				m.statusBar.SetError("Connection lost. Press R to reconnect.")
			} else {
				m.statusBar.SetError(fmt.Sprintf("Transfer failed: %s: %v", evt.Name, evt.Err))
			}
		}
		// Update overlay with progress and job list.
		m.overlay.UpdateProgress(evt)
		if m.engine != nil {
			m.overlay.SetJobs(m.engine.Jobs())
			// Auto-show overlay when transfers start.
			if !m.overlay.IsVisible() && m.engine.ActiveCount() > 0 {
				m.overlay.SetVisible(true)
			}
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
			// If we were in sync state, return to browser.
			if m.state == stateSync {
				m.state = stateBrowser
				return m, tea.Batch(
					m.localPane.Refresh(),
					m.remotePane.Refresh(),
					listenForProgress(m.engine.Progress()),
				)
			}
			return m, tea.Batch(
				m.localPane.Refresh(),
				m.remotePane.Refresh(),
				listenForProgress(m.engine.Progress()),
			)
		}
		return m, listenForProgress(m.engine.Progress())

	case diff.SyncStartMsg:
		m.state = stateSync
		m.diffView.SetEntries(msg.Entries, msg.HasRsync)
		m.diffView.SetSize(m.width, m.height)
		return m, nil

	case diff.SyncCompleteMsg:
		m.state = stateBrowser
		if msg.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Sync error: %v", msg.Err))
		} else {
			m.statusBar.SetError("Sync complete")
		}
		return m, tea.Batch(m.localPane.Refresh(), m.remotePane.Refresh(), clearErrorAfter(5*time.Second))

	case diff.SyncAction:
		return m.handleSyncAction(msg)

	case clearErrorMsg:
		m.statusBar.SetError("")
		return m, nil

	case reconnectMsg:
		if msg.err != nil {
			m.statusBar.SetError(fmt.Sprintf("Reconnect failed: %v", msg.err))
			return m, clearErrorAfter(5 * time.Second)
		}
		// Reconnect succeeded — rebuild remote FS and pane.
		m.conn = msg.conn
		remoteFS, err := fs.NewRemoteFS(m.conn.Client)
		if err != nil {
			m.statusBar.SetError(fmt.Sprintf("Reconnect sftp error: %v", err))
			return m, clearErrorAfter(5 * time.Second)
		}
		m.remotePane = pane.New(remoteFS, "Remote")
		m.remotePane.SetActive(m.activePane == 1)
		m.setPaneSizes()
		cfg := m.conn.Config
		connInfo := fmt.Sprintf("%s@%s:%s", cfg.User, cfg.HostName, cfg.Port)
		m.statusBar.SetConnection(connInfo)
		m.statusBar.SetError("Reconnected")
		return m, tea.Batch(m.remotePane.Init(), clearErrorAfter(3*time.Second))
	}

	switch m.state {
	case statePicker:
		return m.updatePicker(msg)
	case stateConnecting:
		return m.updateConnecting(msg)
	case stateBrowser:
		return m.updateBrowser(msg)
	case stateSync:
		return m.updateSync(msg)
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
	case stateSync:
		return m.diffView.View()
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
		m.picker.SetError("")
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
		m.picker.SetError(fmt.Sprintf("Connection failed: %v", msg.err))
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

	// Handle Esc: close the highest-priority visible overlay.
	if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "esc" {
		if m.helpOverlay.IsVisible() {
			m.helpOverlay.SetVisible(false)
			return m, nil
		}
		if m.overlay.IsVisible() {
			m.overlay.SetVisible(false)
			return m, nil
		}
		if m.infoPanel.IsVisible() {
			m.infoPanel.SetVisible(false)
			return m, nil
		}
	}

	// Handle overlay-specific keys when transfer overlay is visible.
	if m.overlay.IsVisible() {
		if msg, ok := msg.(tea.KeyMsg); ok {
			switch msg.String() {
			case "t":
				m.overlay.Toggle()
				return m, nil
			}
		}
	}

	// Block pane navigation when help overlay is visible.
	if m.helpOverlay.IsVisible() {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		defer func() { m.lastKey = key }()

		switch key {
		case "t":
			m.overlay.Toggle()
			return m, nil

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

		case "e":
			// edit file
			return m.startEdit()

		case "S":
			// sync/diff view
			return m.startSync()

		case "i":
			// toggle info panel
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

		case "?":
			// toggle help overlay
			m.helpOverlay.Toggle()
			return m, nil

		case "R":
			// Reconnect to the remote host.
			if m.conn != nil {
				m.statusBar.SetError("Reconnecting...")
				return m, m.reconnect()
			}
		}

	case editor.EditSessionReadyMsg:
		if msg.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Edit error: %v", msg.Err))
			return m, nil
		}
		m.editSession = msg.Session
		m.statusBar.SetError(fmt.Sprintf("Editing %s...", filepath.Base(msg.Session.RemotePath)))
		return m, editor.OpenEditor(msg.Session)

	case editor.EditorExitMsg:
		if msg.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Editor error: %v", msg.Err))
			if msg.Session != nil {
				editor.Cleanup(msg.Session)
			}
			m.editSession = nil
			return m, nil
		}
		// Editor closed — check for changes and upload.
		return m, editor.CheckAndUpload(msg.Session)

	case editor.EditCompleteMsg:
		m.editSession = nil
		if msg.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Edit error: %v", msg.Err))
			return m, clearErrorAfter(5 * time.Second)
		} else if msg.Modified {
			m.statusBar.SetError("File uploaded successfully")
			// Refresh the remote pane to reflect changes.
			return m, tea.Batch(m.remotePane.Refresh(), clearErrorAfter(3*time.Second))
		} else {
			m.statusBar.SetError("")
		}
		return m, nil

	case editor.ConflictMsg:
		// Remote file changed since download — show conflict modal.
		m.editSession = msg.Session
		m.inputMode = "confirm-conflict"
		m.statusBar.SetError("Remote file changed! (o)verwrite / (r)e-download / (a)bort")
		return m, nil

	case editor.UploadCompleteMsg:
		m.editSession = nil
		if msg.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Upload error: %v", msg.Err))
		} else {
			m.statusBar.SetError("File uploaded (overwritten)")
			return m, m.remotePane.Refresh()
		}
		return m, nil
	}

	// Route key messages to active pane only; route all other messages
	// (async responses like entriesMsg, errMsg) to BOTH panes so each
	// pane can claim its own async results.
	var cmd tea.Cmd
	if _, isKey := msg.(tea.KeyMsg); isKey {
		if m.activePane == 0 {
			m.localPane, cmd = m.localPane.Update(msg)
		} else {
			m.remotePane, cmd = m.remotePane.Update(msg)
		}
	} else {
		var cmd1, cmd2 tea.Cmd
		m.localPane, cmd1 = m.localPane.Update(msg)
		m.remotePane, cmd2 = m.remotePane.Update(msg)
		cmd = tea.Batch(cmd1, cmd2)
	}
	m.updateStatusSelection()
	return m, cmd
}

// --- State: Sync ---

func (m Model) updateSync(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			m.state = stateBrowser
			return m, tea.Batch(m.localPane.Refresh(), m.remotePane.Refresh())
		}
	}

	var cmd tea.Cmd
	m.diffView, cmd = m.diffView.Update(msg)
	return m, cmd
}

// startSync kicks off an async comparison of local and remote directories.
func (m Model) startSync() (tea.Model, tea.Cmd) {
	localFS := m.localPane.FS()
	localPath := m.localPane.Path()
	remoteFS := m.remotePane.FS()
	remotePath := m.remotePane.Path()
	sshClient := m.conn.Client

	m.statusBar.SetError("Comparing directories...")

	return m, func() tea.Msg {
		entries, err := transfer.Compare(localFS, localPath, remoteFS, remotePath)
		if err != nil {
			return diff.SyncCompleteMsg{Err: err}
		}
		hasRsync := transfer.HasRsync(sshClient)
		return diff.SyncStartMsg{Entries: entries, HasRsync: hasRsync}
	}
}

// handleSyncAction processes a push/pull request from the diff view.
func (m Model) handleSyncAction(action diff.SyncAction) (tea.Model, tea.Cmd) {
	localFS := m.localPane.FS()
	localPath := m.localPane.Path()
	remoteFS := m.remotePane.FS()
	remotePath := m.remotePane.Path()

	m.engine = transfer.NewEngine(2)

	for _, de := range action.Entries {
		var srcFS, dstFS fs.FileSystem
		var srcBase, dstBase string

		if action.Direction == "push" {
			// Local -> Remote
			srcFS = localFS
			srcBase = localPath
			dstFS = remoteFS
			dstBase = remotePath
		} else {
			// Remote -> Local
			srcFS = remoteFS
			srcBase = remotePath
			dstFS = localFS
			dstBase = localPath
		}

		// Build the entry to enqueue.
		var entry fs.Entry
		if action.Direction == "push" && de.LocalEntry != nil {
			entry = *de.LocalEntry
		} else if action.Direction == "pull" && de.RemoteEntry != nil {
			entry = *de.RemoteEntry
		} else {
			continue
		}

		m.engine.EnqueueEntry(entry, srcFS, srcBase, dstFS, dstBase)
	}

	go m.engine.Start()

	m.statusBar.SetError(fmt.Sprintf("Syncing %d item(s) %s...", len(action.Entries), action.Direction))

	// Stay in sync view but listen for progress.
	return m, listenForProgress(m.engine.Progress())
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

		if m.inputMode == "confirm-conflict" {
			switch msg.String() {
			case "o":
				// Overwrite: force upload.
				m.inputMode = ""
				return m, editor.ForceUpload(m.editSession)
			case "r":
				// Re-download: clean up and start over.
				m.inputMode = ""
				session := m.editSession
				m.editSession = nil
				remotePath := session.RemotePath
				remoteFS := session.RemoteFS
				editor.Cleanup(session)
				return m, editor.StartEdit(remoteFS, remotePath)
			case "a":
				// Abort: clean up temp files.
				m.inputMode = ""
				editor.Cleanup(m.editSession)
				m.editSession = nil
				m.statusBar.SetError("Edit aborted")
				return m, nil
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

	return m, clearErrorAfter(3 * time.Second)
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

// startEdit opens the current file in the user's editor.
func (m Model) startEdit() (tea.Model, tea.Cmd) {
	var entry *fs.Entry
	if m.activePane == 0 {
		entry = m.localPane.CurrentEntry()
	} else {
		entry = m.remotePane.CurrentEntry()
	}
	if entry == nil || entry.IsDir {
		return m, nil
	}

	if m.activePane == 0 {
		// Local file: open directly.
		return m, editor.EditLocal(entry.Path)
	}

	// Remote file: download, edit, upload.
	m.statusBar.SetError(fmt.Sprintf("Downloading %s...", entry.Name))
	return m, editor.StartEdit(m.remotePane.FS(), entry.Path)
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

	// Build the base layout: panes + optional info panel + status bar.
	var sections []string
	sections = append(sections, panes)
	if m.infoPanel.IsVisible() {
		sections = append(sections, m.infoPanel.View())
	}
	sections = append(sections, bar)
	base := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Render full-screen overlays on top (highest priority first).
	if m.helpOverlay.IsVisible() {
		return m.helpOverlay.View()
	}
	if m.overlay.IsVisible() {
		return m.overlay.View()
	}

	return base
}

// --- Helpers ---

func (m *Model) handleResize() (tea.Model, tea.Cmd) {
	switch m.state {
	case statePicker:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		return m, cmd
	case stateConnecting:
		// No special layout needed, but store the size.
	case stateBrowser:
		m.setPaneSizes()
	case stateSync:
		m.diffView.SetSize(m.width, m.height)
	}
	return m, nil
}

func (m *Model) setPaneSizes() {
	paneWidth := (m.width) / 2
	paneHeight := m.height - 1 // reserve 1 row for status bar

	// Reserve space for info panel if visible.
	infoPanelHeight := m.infoPanel.Height()
	paneHeight -= infoPanelHeight

	m.localPane.SetSize(paneWidth, paneHeight)
	m.remotePane.SetSize(m.width-paneWidth, paneHeight)
	m.statusBar.SetWidth(m.width)
	m.overlay.SetSize(m.width, m.height)
	m.infoPanel.SetSize(m.width, infoPanelHeight)
	m.helpOverlay.SetSize(m.width, m.height)
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

	// Keep info panel entry in sync with cursor.
	if m.infoPanel.IsVisible() {
		if m.activePane == 0 {
			m.infoPanel.SetEntry(m.localPane.CurrentEntry())
		} else {
			m.infoPanel.SetEntry(m.remotePane.CurrentEntry())
		}
	}
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

// clearErrorAfter returns a Cmd that sends a clearErrorMsg after the given duration.
func clearErrorAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

// reconnect attempts to re-establish the SSH connection using the same host.
func (m Model) reconnect() tea.Cmd {
	host := m.conn.Host()
	return func() tea.Msg {
		conn, err := ferrySSH.Connect(ferrySSH.ConnectOptions{
			Host: host,
		})
		if err != nil {
			return reconnectMsg{err: err}
		}
		return reconnectMsg{conn: conn}
	}
}

// isConnectionError returns true if the error looks like a dropped connection.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, io.EOF.Error())
}
