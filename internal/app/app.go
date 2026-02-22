// internal/app/app.go
package app

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andrewstuart/ferry/internal/fs"
	ferrySSH "github.com/andrewstuart/ferry/internal/ssh"
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

// Model is the top-level Bubble Tea model for ferry.
type Model struct {
	state appState
	width int
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

	// Error
	err error
}

// New creates the initial app model.
func New() Model {
	hosts, _ := ferrySSH.ParseConfigHosts(ferrySSH.DefaultConfigPath())

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Cyan)

	return Model{
		state:   statePicker,
		picker:  picker.New(hosts),
		spinner: sp,
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
			if m.conn != nil {
				m.conn.Close()
			}
			return m, tea.Quit
		case "q":
			if m.state == stateBrowser {
				if m.conn != nil {
					m.conn.Close()
				}
				return m, tea.Quit
			}
		}
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.activePane = 1 - m.activePane
			m.localPane.SetActive(m.activePane == 0)
			m.remotePane.SetActive(m.activePane == 1)
			m.updateStatusSelection()
			return m, nil
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

func (m Model) viewBrowser() string {
	leftView := m.localPane.View()
	rightView := m.remotePane.View()

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
	bar := m.statusBar.View()

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
