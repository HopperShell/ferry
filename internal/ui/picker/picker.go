// internal/ui/picker/picker.go
package picker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	s3util "github.com/HopperShell/ferry/internal/s3"
	ferrySSH "github.com/HopperShell/ferry/internal/ssh"
	"github.com/HopperShell/ferry/internal/ui/theme"
)

type ConnectionTarget struct {
	Type    string // "ssh" or "s3"
	Host    string // SSH host (for ssh type)
	Bucket  string // S3 bucket name (for s3 type)
	Prefix  string // S3 prefix (for s3 type)
	Profile string // AWS profile (for s3 type)
}

type HostSelected struct {
	Host string
}

type TargetSelected struct {
	Target ConnectionTarget
}

type Model struct {
	hosts           []ferrySSH.HostEntry
	s3Buckets       []s3util.BucketEntry
	filtered        []ferrySSH.HostEntry
	filteredBuckets []s3util.BucketEntry
	input           textinput.Model
	cursor          int
	width           int
	height          int
	errMsg          string
}

// SetError sets an error message to display in the picker.
func (m *Model) SetError(msg string) {
	m.errMsg = msg
}

// totalItems returns the total number of selectable items (filtered hosts + filtered buckets).
func (m Model) totalItems() int {
	return len(m.filtered) + len(m.filteredBuckets)
}

func NewWithBuckets(hosts []ferrySSH.HostEntry, buckets []s3util.BucketEntry) Model {
	ti := textinput.New()
	ti.Placeholder = "Search hosts or enter user@host:port or s3://bucket..."
	ti.Focus()
	ti.CharLimit = 256

	return Model{
		hosts:           hosts,
		s3Buckets:       buckets,
		filtered:        hosts,
		filteredBuckets: buckets,
		input:           ti,
	}
}

func New(hosts []ferrySSH.HostEntry) Model {
	return NewWithBuckets(hosts, nil)
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return m, m.handleEnter()
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+j":
			if m.cursor < m.totalItems()-1 {
				m.cursor++
			}
		case "esc":
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	query := m.input.Value()
	if query == "" {
		m.filtered = m.hosts
		m.filteredBuckets = m.s3Buckets
	} else {
		// Filter SSH hosts
		names := make([]string, len(m.hosts))
		for i, h := range m.hosts {
			names[i] = fmt.Sprintf("%s %s %s", h.Name, h.HostName, h.User)
		}
		matches := fuzzy.Find(query, names)
		m.filtered = make([]ferrySSH.HostEntry, len(matches))
		for i, match := range matches {
			m.filtered[i] = m.hosts[match.Index]
		}

		// Filter S3 buckets
		if len(m.s3Buckets) > 0 {
			bucketNames := make([]string, len(m.s3Buckets))
			for i, b := range m.s3Buckets {
				bucketNames[i] = b.Name + " " + b.Profile
			}
			bucketMatches := fuzzy.Find(query, bucketNames)
			m.filteredBuckets = make([]s3util.BucketEntry, len(bucketMatches))
			for i, match := range bucketMatches {
				m.filteredBuckets[i] = m.s3Buckets[match.Index]
			}
		} else {
			m.filteredBuckets = nil
		}
	}
	if m.cursor >= m.totalItems() {
		m.cursor = max(0, m.totalItems()-1)
	}

	return m, cmd
}

// handleEnter processes the enter key, returning the appropriate command.
func (m Model) handleEnter() tea.Cmd {
	// Check if the input is an s3:// URI
	val := m.input.Value()
	if strings.HasPrefix(val, "s3://") {
		path := strings.TrimPrefix(val, "s3://")
		bucket, prefix, _ := strings.Cut(path, "/")
		return selectTarget(ConnectionTarget{
			Type:   "s3",
			Bucket: bucket,
			Prefix: prefix,
		})
	}

	total := m.totalItems()
	if total > 0 && m.cursor < total {
		// Determine if the cursor is on an SSH host or S3 bucket
		if m.cursor < len(m.filtered) {
			return selectTarget(ConnectionTarget{
				Type: "ssh",
				Host: m.filtered[m.cursor].Name,
			})
		}
		bucketIdx := m.cursor - len(m.filtered)
		if bucketIdx < len(m.filteredBuckets) {
			b := m.filteredBuckets[bucketIdx]
			return selectTarget(ConnectionTarget{
				Type:    "s3",
				Bucket:  b.Name,
				Profile: b.Profile,
			})
		}
	}

	// Fallback: treat raw input as SSH host
	if val != "" {
		return selectTarget(ConnectionTarget{
			Type: "ssh",
			Host: val,
		})
	}
	return nil
}

func (m Model) View() string {
	var b strings.Builder

	logo := lipgloss.NewStyle().Foreground(theme.Cyan).Bold(true).Render(theme.Logo)
	tagline := lipgloss.NewStyle().Foreground(theme.Dim).Render(theme.Tagline)
	b.WriteString(logo)
	b.WriteString(tagline + "\n\n")

	b.WriteString(m.input.View() + "\n\n")

	maxVisible := m.height - 14
	if maxVisible < 3 {
		maxVisible = 3
	}

	// Build a unified list of display lines with their selectable index
	type displayLine struct {
		text       string
		selectable bool
		index      int // index in the combined selectable list
	}

	var lines []displayLine

	// SSH hosts
	for i, h := range m.filtered {
		host := h.HostName
		if host == "" {
			host = h.Name
		}
		user := h.User
		if user == "" {
			user = "~"
		}
		port := h.Port
		if port == "" || port == "22" {
			port = ""
		} else {
			port = ":" + port
		}
		text := fmt.Sprintf("  %s  %s@%s%s", h.Name, user, host, port)
		lines = append(lines, displayLine{text: text, selectable: true, index: i})
	}

	// S3 buckets grouped by profile
	if len(m.filteredBuckets) > 0 {
		lastProfile := ""
		for i, bucket := range m.filteredBuckets {
			profile := bucket.Profile
			if profile == "" {
				profile = "default"
			}
			if profile != lastProfile {
				divider := lipgloss.NewStyle().Foreground(theme.Dim).Render(
					fmt.Sprintf("  ── S3: %s ──", profile))
				lines = append(lines, displayLine{text: divider, selectable: false, index: -1})
				lastProfile = profile
			}
			text := fmt.Sprintf("  s3://%s", bucket.Name)
			lines = append(lines, displayLine{text: text, selectable: true, index: len(m.filtered) + i})
		}
	}

	// Scrolling: find the display-line index of the cursor
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

	for i := start; i < len(lines) && i < start+maxVisible; i++ {
		l := lines[i]
		if !l.selectable {
			b.WriteString(l.text + "\n")
			continue
		}

		line := l.text
		if l.index == m.cursor {
			line = theme.CursorStyle.Render("> " + line[2:])
		} else {
			line = lipgloss.NewStyle().Foreground(theme.White).Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	if m.errMsg != "" {
		errLine := lipgloss.NewStyle().Foreground(theme.Red).Bold(true).Render("  " + m.errMsg)
		b.WriteString(errLine + "\n")
	}
	footer := lipgloss.NewStyle().Foreground(theme.Dim).Render("  enter:connect  esc:quit")
	b.WriteString(footer)

	return b.String()
}

func selectTarget(target ConnectionTarget) tea.Cmd {
	return func() tea.Msg {
		return TargetSelected{Target: target}
	}
}

func selectHost(host string) tea.Cmd {
	return func() tea.Msg {
		return HostSelected{Host: host}
	}
}
