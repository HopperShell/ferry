// internal/ui/transfer/overlay.go
package transfer

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	transferEngine "github.com/andrewstuart/ferry/internal/transfer"
	"github.com/andrewstuart/ferry/internal/ui/theme"
)

// Overlay renders a transfer progress modal over the dual-pane browser.
type Overlay struct {
	jobs     []*transferEngine.Job
	progress map[string]transferEngine.ProgressEvent // jobID -> latest event
	width    int
	height   int
	visible  bool
}

// NewOverlay creates a new transfer progress overlay.
func NewOverlay() *Overlay {
	return &Overlay{
		progress: make(map[string]transferEngine.ProgressEvent),
	}
}

// SetSize updates the available terminal size for centering.
func (o *Overlay) SetSize(width, height int) {
	o.width = width
	o.height = height
}

// SetVisible sets overlay visibility.
func (o *Overlay) SetVisible(visible bool) {
	o.visible = visible
}

// IsVisible returns whether the overlay is currently shown.
func (o *Overlay) IsVisible() bool {
	return o.visible
}

// Toggle flips overlay visibility.
func (o *Overlay) Toggle() {
	o.visible = !o.visible
}

// UpdateProgress updates the latest progress for a job.
func (o *Overlay) UpdateProgress(event transferEngine.ProgressEvent) {
	o.progress[event.JobID] = event
}

// SetJobs updates the job list.
func (o *Overlay) SetJobs(jobs []*transferEngine.Job) {
	o.jobs = jobs
}

// View renders the overlay as a centered modal.
func (o *Overlay) View() string {
	if !o.visible || len(o.jobs) == 0 {
		return ""
	}

	const maxNameLen = 36
	const barWidth = 16
	// Determine box inner width.
	innerWidth := 68
	if o.width > 0 && o.width < innerWidth+4 {
		innerWidth = o.width - 4
	}

	var lines []string
	var completedCount int
	var totalRemaining int64
	var totalSpeed float64

	for _, job := range o.jobs {
		evt, hasProgress := o.progress[job.ID]

		name := job.Name
		if len(name) > maxNameLen {
			name = name[:maxNameLen-1] + "~"
		}
		name = fmt.Sprintf("%-*s", maxNameLen, name)

		var line string
		switch job.Status {
		case transferEngine.JobCompleted:
			completedCount++
			bar := strings.Repeat("\u2588", barWidth)
			line = fmt.Sprintf(" %s %s 100%% %s ",
				name,
				lipgloss.NewStyle().Foreground(theme.Amber).Render(bar),
				lipgloss.NewStyle().Foreground(theme.Green).Render("done"),
			)

		case transferEngine.JobFailed:
			completedCount++ // count as "done" for progress
			errMsg := "error"
			if job.Err != nil {
				errMsg = job.Err.Error()
				if len(errMsg) > 15 {
					errMsg = errMsg[:14] + "~"
				}
			}
			bar := strings.Repeat("\u2591", barWidth)
			line = fmt.Sprintf(" %s %s      %s ",
				name,
				lipgloss.NewStyle().Foreground(theme.Dim).Render(bar),
				lipgloss.NewStyle().Foreground(theme.Red).Render(errMsg),
			)

		case transferEngine.JobActive:
			if hasProgress && evt.TotalBytes > 0 {
				pct := float64(evt.BytesSent) / float64(evt.TotalBytes)
				filled := int(pct * float64(barWidth))
				if filled > barWidth {
					filled = barWidth
				}
				empty := barWidth - filled
				bar := lipgloss.NewStyle().Foreground(theme.Amber).Render(strings.Repeat("\u2588", filled)) +
					lipgloss.NewStyle().Foreground(theme.Dim).Render(strings.Repeat("\u2591", empty))
				pctStr := fmt.Sprintf("%3d%%", int(pct*100))
				speedStr := formatSpeed(evt.Speed)
				line = fmt.Sprintf(" %s %s %s %s ", name, bar, pctStr, speedStr)

				// Accumulate for ETA calculation.
				remaining := evt.TotalBytes - evt.BytesSent
				if remaining > 0 {
					totalRemaining += remaining
				}
				if evt.Speed > 0 {
					totalSpeed += evt.Speed
				}
			} else {
				bar := lipgloss.NewStyle().Foreground(theme.Dim).Render(strings.Repeat("\u2591", barWidth))
				line = fmt.Sprintf(" %s %s   0%%       ", name, bar)
			}

		default: // JobPending
			bar := lipgloss.NewStyle().Foreground(theme.Dim).Render(strings.Repeat("\u2591", barWidth))
			line = fmt.Sprintf(" %s %s      %s ",
				name,
				bar,
				lipgloss.NewStyle().Foreground(theme.Dim).Render("queued"),
			)
		}

		lines = append(lines, line)
	}

	// Footer line.
	etaStr := ""
	if totalSpeed > 0 && totalRemaining > 0 {
		etaSec := float64(totalRemaining) / totalSpeed
		etaStr = fmt.Sprintf("ETA: %ds", int(etaSec))
	}
	footer := fmt.Sprintf(" %d/%d files", completedCount, len(o.jobs))
	if etaStr != "" {
		footer += "  |  " + etaStr
	}
	footer += "  |  Esc: close"

	// Build box content.
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	footerStyled := lipgloss.NewStyle().Foreground(theme.Dim).Render(footer)
	content += footerStyled

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Cyan)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Cyan).
		Padding(0, 1).
		Render(
			titleStyle.Render("Transferring") + "\n" + content,
		)

	// Center the box in the terminal.
	if o.width > 0 && o.height > 0 {
		return lipgloss.Place(o.width, o.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}

// formatSpeed returns a human-readable speed string.
func formatSpeed(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= 1<<30:
		return fmt.Sprintf("%.1fGB/s", bytesPerSec/float64(1<<30))
	case bytesPerSec >= 1<<20:
		return fmt.Sprintf("%.1fMB/s", bytesPerSec/float64(1<<20))
	case bytesPerSec >= 1<<10:
		return fmt.Sprintf("%.1fKB/s", bytesPerSec/float64(1<<10))
	default:
		return fmt.Sprintf("%.0fB/s", bytesPerSec)
	}
}
