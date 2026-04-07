package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// RenderGlowTitle renders each rune with a colour based on how far the
// "bright spot" has swept past it. The spot starts before the first char,
// sweeps right, exits after the last char, then the cycle restarts.
func RenderGlowTitle(title string, glowStep int) string {
	palette := styles.GlowPalette
	pLen := len(palette)
	runes := []rune(title)
	n := len(runes)
	if n == 0 {
		return ""
	}
	// Sweep position within cycle [0, n+pLen)
	pos := glowStep % (n + pLen)
	var sb strings.Builder
	for i, r := range runes {
		dist := pos - i // positive = spot has passed char i
		var color lipgloss.Color
		if dist >= 0 && dist < pLen {
			color = palette[pLen-1-dist] // brightest when dist==0
		} else {
			color = palette[0] // dim outside the sweep window
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(color).Italic(true).Render(string(r)))
	}
	return sb.String()
}

// FormatDuration formats a duration as m:ss. Exported for use in model footer.
func FormatDuration(d time.Duration) string {
	secs := int(d.Seconds())
	mins := secs / 60
	secs %= 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// RenderProgressBar renders a flat progress bar: filled portion uses ━, a ●
// marker sits at the playhead, and the remaining portion uses ─.
// step is accepted for API compatibility but not used (no animation needed).
func RenderProgressBar(pos, dur time.Duration, width, step int) string {
	if width <= 0 {
		return ""
	}
	ratio := 0.0
	if dur > 0 {
		ratio = float64(pos) / float64(dur)
		if ratio > 1 {
			ratio = 1
		}
	}
	filled := int(ratio * float64(width))

	// Reserve one cell for the playhead marker (●) unless at the very edges.
	if filled > 0 && filled < width {
		return styles.ProgressBar.Render(strings.Repeat("━", filled-1)+"●") +
			styles.ProgressBg.Render(strings.Repeat("─", width-filled))
	}
	return styles.ProgressBar.Render(strings.Repeat("━", filled)) +
		styles.ProgressBg.Render(strings.Repeat("─", width-filled))
}

func centerLine(s string, width int) string {
	sw := lipgloss.Width(s)
	pad := max(0, (width-sw)/2)
	return strings.Repeat(" ", pad) + s
}
