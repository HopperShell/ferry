// internal/ui/picker/picker.go
package picker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	ferrySSH "github.com/andrewstuart/ferry/internal/ssh"
	"github.com/andrewstuart/ferry/internal/ui/theme"
)

type HostSelected struct {
	Host string
}

type Model struct {
	hosts    []ferrySSH.HostEntry
	filtered []ferrySSH.HostEntry
	input    textinput.Model
	cursor   int
	width    int
	height   int
}

func New(hosts []ferrySSH.HostEntry) Model {
	ti := textinput.New()
	ti.Placeholder = "Search hosts or enter user@host:port..."
	ti.Focus()
	ti.CharLimit = 256

	return Model{
		hosts:    hosts,
		filtered: hosts,
		input:    ti,
	}
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
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				return m, selectHost(m.filtered[m.cursor].Name)
			}
			if m.input.Value() != "" {
				return m, selectHost(m.input.Value())
			}
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+j":
			if m.cursor < len(m.filtered)-1 {
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
	} else {
		names := make([]string, len(m.hosts))
		for i, h := range m.hosts {
			names[i] = fmt.Sprintf("%s %s %s", h.Name, h.HostName, h.User)
		}
		matches := fuzzy.Find(query, names)
		m.filtered = make([]ferrySSH.HostEntry, len(matches))
		for i, match := range matches {
			m.filtered[i] = m.hosts[match.Index]
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}

	return m, cmd
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

	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}

	for i := start; i < len(m.filtered) && i < start+maxVisible; i++ {
		h := m.filtered[i]
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

		line := fmt.Sprintf("  %s  %s@%s%s", h.Name, user, host, port)

		if i == m.cursor {
			line = theme.CursorStyle.Render("> " + line[2:])
		} else {
			line = lipgloss.NewStyle().Foreground(theme.White).Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	footer := lipgloss.NewStyle().Foreground(theme.Dim).Render("  enter:connect  esc:quit")
	b.WriteString(footer)

	return b.String()
}

func selectHost(host string) tea.Cmd {
	return func() tea.Msg {
		return HostSelected{Host: host}
	}
}
