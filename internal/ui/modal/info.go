// internal/ui/modal/info.go
package modal

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/andrewstuart/ferry/internal/fs"
	"github.com/andrewstuart/ferry/internal/ui/theme"
)

// InfoPanel renders file metadata as a bottom panel below the panes.
type InfoPanel struct {
	entry   *fs.Entry
	visible bool
	width   int
	height  int
}

// NewInfoPanel creates a new InfoPanel.
func NewInfoPanel() *InfoPanel {
	return &InfoPanel{}
}

// SetEntry updates the displayed entry.
func (p *InfoPanel) SetEntry(entry *fs.Entry) {
	p.entry = entry
}

// SetVisible sets panel visibility.
func (p *InfoPanel) SetVisible(v bool) {
	p.visible = v
}

// Toggle flips panel visibility.
func (p *InfoPanel) Toggle() {
	p.visible = !p.visible
}

// IsVisible returns whether the panel is currently shown.
func (p *InfoPanel) IsVisible() bool {
	return p.visible
}

// SetSize updates the available size for rendering.
func (p *InfoPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// Height returns how many terminal rows the panel occupies when visible (including border).
func (p *InfoPanel) Height() int {
	if !p.visible || p.entry == nil {
		return 0
	}
	// 8 rows of content + 2 for top/bottom border
	return 10
}

// View renders the info panel.
func (p *InfoPanel) View() string {
	if !p.visible || p.entry == nil {
		return ""
	}

	labelStyle := lipgloss.NewStyle().Foreground(theme.Dim)
	valueStyle := lipgloss.NewStyle().Foreground(theme.White)

	e := p.entry

	fileType := "Regular file"
	if e.IsDir {
		fileType = "Directory"
	} else if e.Mode&os.ModeSymlink != 0 {
		fileType = "Symlink"
	} else if e.Mode&0o111 != 0 {
		fileType = "Executable"
	}

	rows := []struct{ label, value string }{
		{"Name:    ", e.Name},
		{"Path:    ", e.Path},
		{"Type:    ", fileType},
		{"Size:    ", formatSize(e.Size)},
		{"Mode:    ", formatPermissions(e.Mode)},
		{"Owner:   ", e.Owner},
		{"Group:   ", e.Group},
		{"Modified:", e.ModTime.Format("Jan 02 15:04")},
	}

	var lines []string
	for _, r := range rows {
		line := labelStyle.Render(r.label) + " " + valueStyle.Render(r.value)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")

	innerWidth := p.width - 4 // account for border + padding
	if innerWidth < 20 {
		innerWidth = 20
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Padding(0, 1).
		Width(innerWidth).
		Render(
			lipgloss.NewStyle().Bold(true).Foreground(theme.Cyan).Render("File Info") + "\n" + content,
		)

	return box
}

// formatSize returns a human-readable size string.
func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatPermissions renders an os.FileMode as a -rwxrwxrwx string.
func formatPermissions(mode os.FileMode) string {
	var buf [10]byte

	switch {
	case mode.IsDir():
		buf[0] = 'd'
	case mode&os.ModeSymlink != 0:
		buf[0] = 'l'
	default:
		buf[0] = '-'
	}

	const rwx = "rwx"
	for i := 0; i < 9; i++ {
		if mode&(1<<uint(8-i)) != 0 {
			buf[i+1] = rwx[i%3]
		} else {
			buf[i+1] = '-'
		}
	}

	return string(buf[:])
}
