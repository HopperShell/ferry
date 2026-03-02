// internal/app/app.go
package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/HopperShell/ferry/internal/editor"
	"github.com/HopperShell/ferry/internal/fs"
	s3util "github.com/HopperShell/ferry/internal/s3"
	ferrySSH "github.com/HopperShell/ferry/internal/ssh"
	"github.com/HopperShell/ferry/internal/transfer"
	"github.com/HopperShell/ferry/internal/ui/diff"
	"github.com/HopperShell/ferry/internal/ui/modal"
	"github.com/HopperShell/ferry/internal/ui/pane"
	"github.com/HopperShell/ferry/internal/ui/picker"
	"github.com/HopperShell/ferry/internal/ui/statusbar"
	"github.com/HopperShell/ferry/internal/ui/theme"
)

// appState represents the current UI state.
type appState int

const (
	statePicker     appState = iota // show connection picker
	stateConnecting                 // show spinner while connecting
	statePassword                   // prompt for password
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

// transferReadyMsg signals that an async transfer walk/enqueue has completed.
type transferReadyMsg struct {
	engine *transfer.Engine
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

type s3ConnectSuccessMsg struct {
	client  *s3svc.Client
	bucket  string
	prefix  string
	profile string
}

type s3ConnectErrorMsg struct {
	err error
}

// clipboard stores yanked/cut entries for paste operations.
type clipboard struct {
	entries []fs.Entry
	srcFS   fs.FileSystem
	srcPath string
	cut     bool // true for move (m), false for copy (yy)
}

// pendingPaste stores context for a paste awaiting overwrite confirmation.
type pendingPaste struct {
	dstFS   fs.FileSystem
	dstPath string
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
	conn          *ferrySSH.Connection
	passwordInput textinput.Model

	// Browser
	localPane  pane.Model
	remotePane pane.Model
	activePane int // 0 = left (local), 1 = right (remote)
	statusBar  statusbar.StatusBar
	lastKey    string // for yy/dd detection at app level

	// File operations
	clip         *clipboard
	engine       *transfer.Engine
	inputMode    string // "", "rename", "mkdir", "confirm-delete", "confirm-overwrite"
	inputField   textinput.Model
	pendingPaste *pendingPaste

	// Editor
	editSession *editor.EditSession

	// Info panel and help overlay
	infoPanel   *modal.InfoPanel
	helpOverlay *modal.HelpOverlay

	// Sync/diff view
	diffView       diff.Model
	syncLocalRoot  string // local root used for the active sync comparison
	syncRemoteRoot string // remote root used for the active sync comparison
	syncProgress   chan diff.SyncProgressMsg // progress updates from sync goroutine
	mirrorAction   diff.MirrorAction         // pending mirror direction
	mirrorEntries  []transfer.DiffEntry      // all entries for mirror confirmation

	// S3 backend (nil when using SSH)
	s3Client    *s3svc.Client
	s3Bucket    string
	s3Prefix    string
	s3Profile   string // AWS profile used for connection
	backendType string // "ssh" or "s3"

	// Error
	err error
}

// Options configures how the app starts.
type Options struct {
	Host  string // If set, skip picker and connect to SSH host
	S3URI string // If set, skip picker and connect to S3 (e.g., "s3://bucket/prefix")
}

// New creates the initial app model with the connection picker.
func New() Model {
	return NewWithOptions(Options{})
}

// NewWithOptions creates the initial app model with the given options.
// If opts.Host is set, the picker is skipped and a direct connection is initiated.
func NewWithOptions(opts Options) Model {
	hosts, _ := ferrySSH.ParseConfigHosts(ferrySSH.DefaultConfigPath())
	buckets := s3util.ListAllBuckets(context.Background())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Cyan)

	ti := textinput.New()
	ti.CharLimit = 256

	pw := textinput.New()
	pw.CharLimit = 256
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '•'
	pw.Placeholder = "password"

	m := Model{
		state:         statePicker,
		picker:        picker.NewWithBuckets(hosts, buckets),
		spinner:       sp,
		inputField:    ti,
		passwordInput: pw,
		infoPanel:   modal.NewInfoPanel(),
		helpOverlay: modal.NewHelpOverlay(),
		diffView:    diff.New(),
	}

	if opts.S3URI != "" {
		m.state = stateConnecting
		m.connectHost = opts.S3URI
		m.backendType = "s3"
	} else if opts.Host != "" {
		m.state = stateConnecting
		m.connectHost = opts.Host
		m.backendType = "ssh"
	}

	return m
}

func (m Model) Init() tea.Cmd {
	if m.state == stateConnecting {
		if m.backendType == "s3" {
			return tea.Batch(m.spinner.Tick, m.doS3Connect(m.connectHost, m.s3Profile))
		}
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
		} else if evt.Done {
			m.statusBar.SetError(fmt.Sprintf("Transferred: %s", evt.Name))
		}
		// If this was a move (cut) and all done, delete sources.
		if m.engine != nil && m.engine.IsFinished() && m.clip != nil && m.clip.cut {
			for _, entry := range m.clip.entries {
				_ = m.clip.srcFS.Remove(entry.Path)
			}
			m.clip = nil
		}
		// Keep listening; transferDoneMsg will handle final cleanup when channel closes.
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

	case diff.MirrorAction:
		return m.handleMirrorAction(msg)

	case diff.SyncProgressMsg:
		m.diffView.SetSyncProgress(msg.Done, msg.Total, msg.Name)
		return m, m.listenSyncProgress()

	case diff.SyncRefreshMsg:
		if msg.Err != nil {
			m.statusBar.SetError(fmt.Sprintf("Sync error: %v", msg.Err))
		}
		m.diffView.SetEntries(msg.Entries, msg.HasRsync)
		m.diffView.SetSize(m.width, m.height)
		m.syncProgress = nil
		return m, nil

	case transferReadyMsg:
		m.engine = msg.engine
		m.statusBar.SetError("Transferring files...")
		return m, listenForProgress(m.engine.Progress())

	case transferDoneMsg:
		// Engine finished (channel closed). Clean up and refresh panes.
		jobCount := 0
		if m.engine != nil {
			jobCount = len(m.engine.Jobs())
			m.engine = nil
		}
		if jobCount > 0 {
			m.statusBar.SetError(fmt.Sprintf("Transferred %d item(s)", jobCount))
		} else {
			m.statusBar.SetError("Nothing to transfer")
		}
		return m, tea.Batch(
			m.localPane.Refresh(),
			m.remotePane.Refresh(),
			clearErrorAfter(5*time.Second),
		)

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

	case s3ConnectSuccessMsg:
		m.s3Client = msg.client
		m.s3Bucket = msg.bucket
		m.s3Prefix = msg.prefix
		m.backendType = "s3"
		m.state = stateBrowser

		localFS := fs.NewLocalFS()
		remoteFS := fs.NewS3FS(msg.client, msg.bucket, msg.prefix)

		m.localPane = pane.New(localFS, "Local")
		m.remotePane = pane.New(remoteFS, "S3")
		m.activePane = 0
		m.localPane.SetActive(true)
		m.remotePane.SetActive(false)

		connInfo := fmt.Sprintf("s3://%s", msg.bucket)
		if msg.prefix != "" {
			connInfo += "/" + strings.TrimSuffix(msg.prefix, "/")
		}
		m.statusBar.SetConnection(connInfo)
		m.setPaneSizes()

		return m, tea.Batch(m.localPane.Init(), m.remotePane.Init())

	case s3ConnectErrorMsg:
		m.state = statePicker
		m.err = msg.err
		m.picker.SetError(fmt.Sprintf("S3 connection failed: %v", msg.err))
		return m, nil
	}

	switch m.state {
	case statePicker:
		return m.updatePicker(msg)
	case stateConnecting:
		return m.updateConnecting(msg)
	case statePassword:
		return m.updatePassword(msg)
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
	case statePassword:
		return m.viewPassword()
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
	case picker.TargetSelected:
		m.state = stateConnecting
		m.err = nil
		m.picker.SetError("")
		if msg.Target.Type == "s3" {
			uri := "s3://" + msg.Target.Bucket
			if msg.Target.Prefix != "" {
				uri += "/" + msg.Target.Prefix
			}
			m.connectHost = uri
			m.backendType = "s3"
			m.s3Profile = msg.Target.Profile
			return m, tea.Batch(m.spinner.Tick, m.doS3Connect(uri, msg.Target.Profile))
		}
		m.connectHost = msg.Target.Host
		m.backendType = "ssh"
		return m, tea.Batch(m.spinner.Tick, m.doConnect(msg.Target.Host))
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
		// If auth failed, prompt for password instead of going back to picker.
		if strings.Contains(msg.err.Error(), "unable to authenticate") {
			m.state = statePassword
			m.passwordInput.SetValue("")
			m.passwordInput.Focus()
			return m, textinput.Blink
		}
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

// --- State: Password ---

func (m Model) updatePassword(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			password := m.passwordInput.Value()
			m.passwordInput.SetValue("")
			m.passwordInput.Blur()
			m.state = stateConnecting
			return m, tea.Batch(m.spinner.Tick, m.doConnectWithPassword(m.connectHost, password))
		case "esc":
			m.passwordInput.Blur()
			m.state = statePicker
			return m, nil
		}
	case connectSuccessMsg:
		return m.updateConnecting(msg)
	case connectErrorMsg:
		return m.updateConnecting(msg)
	}

	var cmd tea.Cmd
	m.passwordInput, cmd = m.passwordInput.Update(msg)
	return m, cmd
}

func (m Model) viewPassword() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Cyan)
	dimStyle := lipgloss.NewStyle().Foreground(theme.Dim)

	content := fmt.Sprintf("\n\n   %s\n\n   %s\n   %s\n\n   %s",
		titleStyle.Render(fmt.Sprintf("Password for %s", m.connectHost)),
		m.passwordInput.View(),
		"",
		dimStyle.Render("Enter: submit  Esc: cancel"),
	)
	return content
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
		if m.infoPanel.IsVisible() {
			m.infoPanel.SetVisible(false)
			return m, nil
		}
	}

	// Block pane navigation when help overlay is visible.
	if m.helpOverlay.IsVisible() {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, nil
		}
	}

	// When the active pane is capturing text input (search or find mode),
	// forward key events directly to the pane, skipping app-level bindings.
	if _, isKey := msg.(tea.KeyMsg); isKey {
		active := m.localPane.InputActive()
		if m.activePane == 1 {
			active = m.remotePane.InputActive()
		}
		if active {
			var cmd tea.Cmd
			if m.activePane == 0 {
				m.localPane, cmd = m.localPane.Update(msg)
			} else {
				m.remotePane, cmd = m.remotePane.Update(msg)
			}
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()

		switch key {
		case "tab":
			m.lastKey = key
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
			m.lastKey = key
			return m, nil

		case "p":
			m.lastKey = key
			// paste
			return m.doPaste()

		case "d":
			if m.lastKey == "d" {
				// dd: delete with confirmation
				m.lastKey = ""
				return m.startDelete()
			}
			m.lastKey = key
			return m, nil

		case "r":
			m.lastKey = ""
			return m.startRename()

		case "m":
			m.lastKey = ""
			return m.doYank(true)

		case "D":
			m.lastKey = ""
			return m.startMkdir()

		case "e":
			m.lastKey = ""
			return m.startEdit()

		case "S", "ctrl+s":
			m.lastKey = ""
			return m.startSync()

		case "i":
			m.lastKey = ""
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
			m.lastKey = ""
			m.helpOverlay.Toggle()
			return m, nil

		case "R":
			m.lastKey = ""
			if m.backendType == "ssh" && m.conn != nil {
				m.statusBar.SetError("Reconnecting...")
				return m, m.reconnect()
			}

		default:
			m.lastKey = ""
		}

	case pane.TransferRequestMsg:
		// Immediate transfer to the other pane.
		var srcFS fs.FileSystem
		var srcPath string
		var dstFS fs.FileSystem
		var dstPath string
		if m.activePane == 0 {
			srcFS = m.localPane.FS()
			srcPath = m.localPane.Path()
			dstFS = m.remotePane.FS()
			dstPath = m.remotePane.Path()
		} else {
			srcFS = m.remotePane.FS()
			srcPath = m.remotePane.Path()
			dstFS = m.localPane.FS()
			dstPath = m.localPane.Path()
		}
		engine := transfer.NewEngine(3, true)
		go engine.Start()
		entries := msg.Entries
		go func() {
			for _, entry := range entries {
				engine.EnqueueEntry(entry, srcFS, srcPath, dstFS, dstPath)
			}
			engine.Done()
		}()
		m.engine = engine
		m.statusBar.SetError(fmt.Sprintf("Transferring %d item(s)...", len(entries)))
		return m, listenForProgress(engine.Progress())

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
	if m.inputMode == "confirm-mirror" {
		return m.updateInputMode(msg)
	}

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
// If a directory is selected or under the cursor in the active pane, it
// compares that directory against the matching path on the other side.
// Otherwise it compares the two panes' current directories.
func (m Model) startSync() (tea.Model, tea.Cmd) {
	localFS := m.localPane.FS()
	localPath := m.localPane.Path()
	remoteFS := m.remotePane.FS()
	remotePath := m.remotePane.Path()

	hasRsync := false
	if m.backendType == "ssh" && m.conn != nil {
		hasRsync = transfer.HasRsync(m.conn.Client)
	}

	// If cursor is on a directory, sync that specific folder against the
	// same-named folder on the other side.
	var curEntry *fs.Entry
	if m.activePane == 0 {
		curEntry = m.localPane.CurrentEntry()
	} else {
		curEntry = m.remotePane.CurrentEntry()
	}
	scope := filepath.Base(localPath) // default: comparing the pane root
	if curEntry != nil && curEntry.IsDir {
		entry := curEntry
		dirName := entry.Name
		scope = dirName
		if m.activePane == 0 {
			localPath = entry.Path
			// Only append dirName to remote if it doesn't already end with it.
			if filepath.Base(remotePath) != dirName {
				remotePath = filepath.Join(remotePath, dirName)
			}
		} else {
			remotePath = entry.Path
			// Only append dirName to local if it doesn't already end with it.
			if filepath.Base(localPath) != dirName {
				localPath = filepath.Join(localPath, dirName)
			}
		}
	}

	// Store sync roots so handleSyncAction uses the correct base paths.
	m.syncLocalRoot = localPath
	m.syncRemoteRoot = remotePath
	m.diffView.SetScope(scope)

	m.statusBar.SetError(fmt.Sprintf("Comparing %s ...", scope))

	return m, func() tea.Msg {
		entries, err := transfer.Compare(localFS, localPath, remoteFS, remotePath)
		if err != nil {
			return diff.SyncCompleteMsg{Err: err}
		}
		return diff.SyncStartMsg{Entries: entries, HasRsync: hasRsync}
	}
}

// handleSyncAction processes a push/pull request from the diff view.
// Transfers all items in a background goroutine, sending progress after each.
// When all differing files are selected and the remote has rsync, uses rsync
// for the transfer instead of file-by-file SFTP.
func (m Model) handleSyncAction(action diff.SyncAction) (tea.Model, tea.Cmd) {
	localFS := m.localPane.FS()
	remoteFS := m.remotePane.FS()
	localRoot := m.syncLocalRoot
	remoteRoot := m.syncRemoteRoot
	direction := action.Direction
	diffEntries := action.Entries
	total := len(diffEntries)

	// Use rsync when all differing entries are selected and the remote supports it.
	if m.diffView.HasRsync() && len(diffEntries) == m.diffView.DiffCount() && m.backendType == "ssh" && m.conn != nil {
		progress := make(chan diff.SyncProgressMsg, 1)
		m.syncProgress = progress
		m.diffView.SetSyncing("rsync starting...")

		host := m.conn.Host()
		go func() {
			rsyncProgress := make(chan string, 64)
			errCh := make(chan error, 1)

			go func() {
				if direction == "push" {
					errCh <- transfer.RsyncPush(localRoot, remoteRoot, host, rsyncProgress)
				} else {
					errCh <- transfer.RsyncPull(remoteRoot, localRoot, host, rsyncProgress)
				}
			}()

			for line := range rsyncProgress {
				progress <- diff.SyncProgressMsg{Done: 0, Total: 0, Name: line}
			}

			if err := <-errCh; err != nil {
				progress <- diff.SyncProgressMsg{Done: 0, Total: 0, Name: "rsync error: " + err.Error()}
			}
			close(progress)
		}()

		return m, m.listenSyncProgress()
	}

	progress := make(chan diff.SyncProgressMsg, total)
	m.syncProgress = progress

	m.diffView.SetSyncing(fmt.Sprintf("0/%d", total))

	go func() {
		for i, de := range diffEntries {
			var srcFS, dstFS fs.FileSystem
			var srcRoot, dstRoot string

			if direction == "push" {
				srcFS = localFS
				srcRoot = localRoot
				dstFS = remoteFS
				dstRoot = remoteRoot
			} else {
				srcFS = remoteFS
				srcRoot = remoteRoot
				dstFS = localFS
				dstRoot = localRoot
			}

			dstPath := filepath.Join(dstRoot, de.RelPath)

			if de.IsDir {
				_ = dstFS.Mkdir(dstPath, 0o755)
			} else {
				srcEntry := de.LocalEntry
				if direction == "pull" {
					srcEntry = de.RemoteEntry
				}
				if srcEntry != nil {
					_ = dstFS.Mkdir(filepath.Dir(dstPath), 0o755)
					srcPath := filepath.Join(srcRoot, de.RelPath)
					if err := copyFile(srcFS, srcPath, dstFS, dstPath); err != nil {
						// Continue on error — report in progress name.
						progress <- diff.SyncProgressMsg{Done: i + 1, Total: total, Name: fmt.Sprintf("%s (%v)", de.RelPath, err)}
						continue
					}
					if stat, err := srcFS.Stat(srcPath); err == nil && !stat.ModTime.IsZero() {
						_ = dstFS.Chtimes(dstPath, stat.ModTime)
					}
				}
			}

			progress <- diff.SyncProgressMsg{Done: i + 1, Total: total, Name: de.RelPath}
		}
		close(progress)
	}()

	return m, m.listenSyncProgress()
}

// handleMirrorAction sets up the confirmation prompt for a mirror operation.
func (m Model) handleMirrorAction(action diff.MirrorAction) (tea.Model, tea.Cmd) {
	entries := m.diffView.DiffEntries()
	m.mirrorAction = action
	m.mirrorEntries = entries

	// Count copies and deletes for the confirmation message.
	var copyCount, deleteCount int
	for _, de := range entries {
		if de.Status == transfer.DiffSame {
			continue
		}
		if action.Direction == "push" {
			switch de.Status {
			case transfer.DiffLocalOnly, transfer.DiffModified:
				copyCount++
			case transfer.DiffRemoteOnly:
				deleteCount++
			}
		} else {
			switch de.Status {
			case transfer.DiffRemoteOnly, transfer.DiffModified:
				copyCount++
			case transfer.DiffLocalOnly:
				deleteCount++
			}
		}
	}

	target := "remote"
	if action.Direction == "pull" {
		target = "local"
	}
	m.inputMode = "confirm-mirror"
	m.diffView.SetConfirmMsg(fmt.Sprintf("Mirror %s: copy %d, delete %d %s-only files. y/n?", action.Direction, copyCount, deleteCount, target))
	return m, nil
}

// executeMirror performs the mirror operation: copies differing files and deletes orphans.
// When rsync is available on SSH connections, delegates to rsync --delete.
func (m Model) executeMirror() (tea.Model, tea.Cmd) {
	localFS := m.localPane.FS()
	remoteFS := m.remotePane.FS()
	localRoot := m.syncLocalRoot
	remoteRoot := m.syncRemoteRoot
	direction := m.mirrorAction.Direction
	entries := m.mirrorEntries

	// Use rsync --delete when available on SSH connections.
	if m.diffView.HasRsync() && m.backendType == "ssh" && m.conn != nil {
		progress := make(chan diff.SyncProgressMsg, 1)
		m.syncProgress = progress
		m.diffView.SetSyncing("rsync mirror starting...")

		host := m.conn.Host()
		go func() {
			rsyncProgress := make(chan string, 64)
			errCh := make(chan error, 1)

			go func() {
				if direction == "push" {
					errCh <- transfer.RsyncMirrorPush(localRoot, remoteRoot, host, rsyncProgress)
				} else {
					errCh <- transfer.RsyncMirrorPull(remoteRoot, localRoot, host, rsyncProgress)
				}
			}()

			for line := range rsyncProgress {
				progress <- diff.SyncProgressMsg{Done: 0, Total: 0, Name: line}
			}

			if err := <-errCh; err != nil {
				progress <- diff.SyncProgressMsg{Done: 0, Total: 0, Name: "rsync error: " + err.Error()}
			}
			close(progress)
		}()

		return m, m.listenSyncProgress()
	}

	// Separate into copies and deletes.
	var toCopy []transfer.DiffEntry
	var toDelete []transfer.DiffEntry
	for _, de := range entries {
		if de.Status == transfer.DiffSame {
			continue
		}
		if direction == "push" {
			switch de.Status {
			case transfer.DiffLocalOnly, transfer.DiffModified:
				toCopy = append(toCopy, de)
			case transfer.DiffRemoteOnly:
				toDelete = append(toDelete, de)
			}
		} else {
			switch de.Status {
			case transfer.DiffRemoteOnly, transfer.DiffModified:
				toCopy = append(toCopy, de)
			case transfer.DiffLocalOnly:
				toDelete = append(toDelete, de)
			}
		}
	}

	total := len(toCopy) + len(toDelete)
	progress := make(chan diff.SyncProgressMsg, total)
	m.syncProgress = progress
	m.diffView.SetSyncing(fmt.Sprintf("0/%d", total))

	go func() {
		done := 0

		// Delete files first (non-dirs), then directories deepest-first.
		var files, dirs []transfer.DiffEntry
		for _, de := range toDelete {
			if de.IsDir {
				dirs = append(dirs, de)
			} else {
				files = append(files, de)
			}
		}

		// Sort dirs by path depth descending (deepest first).
		sort.Slice(dirs, func(i, j int) bool {
			return strings.Count(dirs[i].RelPath, "/") > strings.Count(dirs[j].RelPath, "/")
		})

		var delFS fs.FileSystem
		var delRoot string
		if direction == "push" {
			delFS = remoteFS
			delRoot = remoteRoot
		} else {
			delFS = localFS
			delRoot = localRoot
		}

		for _, de := range files {
			delPath := filepath.Join(delRoot, de.RelPath)
			_ = delFS.Remove(delPath)
			done++
			progress <- diff.SyncProgressMsg{Done: done, Total: total, Name: "delete " + de.RelPath}
		}
		for _, de := range dirs {
			delPath := filepath.Join(delRoot, de.RelPath)
			_ = delFS.Remove(delPath)
			done++
			progress <- diff.SyncProgressMsg{Done: done, Total: total, Name: "delete " + de.RelPath}
		}

		// Copy differing/missing files to the target side.
		for _, de := range toCopy {
			var srcFS, dstFS fs.FileSystem
			var srcRoot, dstRoot string
			if direction == "push" {
				srcFS = localFS
				srcRoot = localRoot
				dstFS = remoteFS
				dstRoot = remoteRoot
			} else {
				srcFS = remoteFS
				srcRoot = remoteRoot
				dstFS = localFS
				dstRoot = localRoot
			}

			dstPath := filepath.Join(dstRoot, de.RelPath)
			if de.IsDir {
				_ = dstFS.Mkdir(dstPath, 0o755)
			} else {
				srcEntry := de.LocalEntry
				if direction == "pull" {
					srcEntry = de.RemoteEntry
				}
				if srcEntry != nil {
					_ = dstFS.Mkdir(filepath.Dir(dstPath), 0o755)
					srcPath := filepath.Join(srcRoot, de.RelPath)
					if err := copyFile(srcFS, srcPath, dstFS, dstPath); err != nil {
						done++
						progress <- diff.SyncProgressMsg{Done: done, Total: total, Name: fmt.Sprintf("%s (%v)", de.RelPath, err)}
						continue
					}
					if stat, err := srcFS.Stat(filepath.Join(srcRoot, de.RelPath)); err == nil && !stat.ModTime.IsZero() {
						_ = dstFS.Chtimes(dstPath, stat.ModTime)
					}
				}
			}
			done++
			progress <- diff.SyncProgressMsg{Done: done, Total: total, Name: de.RelPath}
		}
		close(progress)
	}()

	return m, m.listenSyncProgress()
}

func (m Model) updateInputMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.inputMode = ""
			m.pendingPaste = nil
			m.diffView.SetConfirmMsg("")
			m.inputField.Blur()
			return m, nil

		case "enter":
			return m.submitInput()

		case "n":
			if m.inputMode == "confirm-delete" || m.inputMode == "confirm-overwrite" || m.inputMode == "confirm-mirror" {
				m.inputMode = ""
				m.pendingPaste = nil
				m.diffView.SetConfirmMsg("")
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

		if m.inputMode == "confirm-mirror" {
			if msg.String() == "y" {
				m.inputMode = ""
				m.diffView.SetConfirmMsg("")
				return m.executeMirror()
			}
			return m, nil
		}

		if m.inputMode == "confirm-overwrite" {
			if msg.String() == "y" {
				pp := m.pendingPaste
				m.pendingPaste = nil
				m.inputMode = ""
				return m.executePaste(pp.dstFS, pp.dstPath)
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
// If any destination files already exist, it prompts for overwrite confirmation.
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

	// Check if any destination files already exist.
	conflicts := 0
	for _, entry := range m.clip.entries {
		relPath, err := filepath.Rel(m.clip.srcPath, entry.Path)
		if err != nil || relPath == "." {
			relPath = entry.Name
		}
		if !entry.IsDir {
			if _, err := dstFS.Stat(filepath.Join(dstPath, relPath)); err == nil {
				conflicts++
			}
		}
	}

	if conflicts > 0 {
		m.pendingPaste = &pendingPaste{dstFS: dstFS, dstPath: dstPath}
		m.inputMode = "confirm-overwrite"
		m.statusBar.SetError(fmt.Sprintf("Overwrite %d existing file(s)? y/n", conflicts))
		return m, nil
	}

	return m.executePaste(dstFS, dstPath)
}

// executePaste starts the transfer engine for a paste operation.
func (m Model) executePaste(dstFS fs.FileSystem, dstPath string) (tea.Model, tea.Cmd) {
	m.engine = transfer.NewEngine(2, true)
	go m.engine.Start()
	clipEntries := m.clip.entries
	clipSrcFS := m.clip.srcFS
	clipSrcPath := m.clip.srcPath
	go func() {
		for _, entry := range clipEntries {
			m.engine.EnqueueEntry(entry, clipSrcFS, clipSrcPath, dstFS, dstPath)
		}
		m.engine.Done()
	}()

	m.statusBar.SetError(fmt.Sprintf("Transferring %d item(s)...", len(clipEntries)))

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

	// Render full-screen overlays on top.
	if m.helpOverlay.IsVisible() {
		return m.helpOverlay.View()
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
	paneHeight := m.height - m.statusBar.Height() // reserve rows for status bar

	// Reserve space for info panel if visible.
	infoPanelHeight := m.infoPanel.Height()
	paneHeight -= infoPanelHeight

	m.localPane.SetSize(paneWidth, paneHeight)
	m.remotePane.SetSize(m.width-paneWidth, paneHeight)
	m.statusBar.SetWidth(m.width)
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
	return m.doConnectWithPassword(host, "")
}

func (m Model) doConnectWithPassword(host, password string) tea.Cmd {
	return func() tea.Msg {
		opts := ferrySSH.ConnectOptions{
			Host: host,
		}
		if password != "" {
			opts.PasswordCallback = func() (string, error) {
				return password, nil
			}
		}
		conn, err := ferrySSH.Connect(opts)
		if err != nil {
			return connectErrorMsg{err: err}
		}
		return connectSuccessMsg{conn: conn}
	}
}

func (m Model) doS3Connect(uri, profile string) tea.Cmd {
	return func() tea.Msg {
		bucket, prefix, err := s3util.ParseS3URI(uri)
		if err != nil {
			return s3ConnectErrorMsg{err: err}
		}
		result, err := s3util.Connect(context.Background(), bucket, "", profile)
		if err != nil {
			return s3ConnectErrorMsg{err: err}
		}
		return s3ConnectSuccessMsg{
			client:  result.Client,
			bucket:  result.Bucket,
			prefix:  prefix,
			profile: profile,
		}
	}
}

// listenForProgress returns a Cmd that reads the next progress event from the channel.
// When the channel is closed (engine finished), it sends transferDoneMsg.
func listenForProgress(ch <-chan transfer.ProgressEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return transferDoneMsg{}
		}
		return progressMsg(event)
	}
}

// listenSyncProgress reads the next sync progress event from the channel.
// When closed (all transfers done), re-runs Compare and returns SyncRefreshMsg.
func (m Model) listenSyncProgress() tea.Cmd {
	ch := m.syncProgress
	if ch == nil {
		return nil
	}
	localFS := m.localPane.FS()
	remoteFS := m.remotePane.FS()
	localRoot := m.syncLocalRoot
	remoteRoot := m.syncRemoteRoot
	hasRsync := m.diffView.HasRsync()

	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			// All done — re-run Compare for final state.
			entries, err := transfer.Compare(localFS, localRoot, remoteFS, remoteRoot)
			return diff.SyncRefreshMsg{Entries: entries, HasRsync: hasRsync, Err: err}
		}
		return event
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

// copyFile reads src and writes it to dst using an atomic temp file.
// If the destination already matches the source (same size and mtime within 2s),
// the copy is skipped.
func copyFile(srcFS fs.FileSystem, srcPath string, dstFS fs.FileSystem, dstPath string) error {
	srcStat, err := srcFS.Stat(srcPath)
	var perm os.FileMode = 0o644
	if err == nil && srcStat.Mode != 0 {
		perm = srcStat.Mode
	}

	var buf bytes.Buffer
	if err := srcFS.Read(srcPath, &buf); err != nil {
		return fmt.Errorf("read: %w", err)
	}

	tmpPath := dstPath + ".ferry-tmp"
	if err := dstFS.Write(tmpPath, &buf, perm); err != nil {
		_ = dstFS.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	// Some SFTP servers reject rename-over-existing; remove dest first.
	_ = dstFS.Remove(dstPath)
	if err := dstFS.Rename(tmpPath, dstPath); err != nil {
		// Rename still failed — write directly as fallback.
		_ = dstFS.Remove(tmpPath)
		buf.Reset()
		if err := srcFS.Read(srcPath, &buf); err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if err := dstFS.Write(dstPath, &buf, perm); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
	return nil
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
