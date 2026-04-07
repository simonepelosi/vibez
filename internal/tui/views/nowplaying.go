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

// wavePattern is the repeating 4-char zigzag used for the progress bar wave.
var wavePattern = []rune{'╱', '╱', '╲', '╲'}

// gradStops are the colour stops for the filled-portion gradient
// (cornflower blue → lavender → rose pink).
var gradStops = []lipgloss.Color{
	styles.ColorProgress,      // #89b4fa — blue
	lipgloss.Color("#cba6f7"), // lavender
	styles.ColorLove,          // #f38ba8 — rose pink
}

// progressGradient returns the gradient colour for position i out of total
// cells, interpolating across gradStops.
func progressGradient(i, total int) lipgloss.Color {
	if total <= 1 || len(gradStops) < 2 {
		return gradStops[0]
	}
	segments := len(gradStops) - 1
	t := float64(i) / float64(total-1) // 0.0 … 1.0
	seg := int(t * float64(segments))
	if seg >= segments {
		return gradStops[len(gradStops)-1]
	}
	local := t*float64(segments) - float64(seg)
	return styles.LerpColor(gradStops[seg], gradStops[seg+1], local)
}

// RenderProgressBar renders a static flat zigzag (╱╱╲╲) progress bar.
// The filled portion is coloured with a blue→lavender→pink gradient;
// the remaining portion uses the muted surface colour with the same zigzag
// so the pattern reads as one continuous line.
func RenderProgressBar(pos, dur time.Duration, width int) string {
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

	n := len(wavePattern)

	var sb strings.Builder
	for i := range width {
		ch := string(wavePattern[i%n])
		if i < filled {
			color := progressGradient(i, filled)
			sb.WriteString(lipgloss.NewStyle().Foreground(color).Render(ch))
		} else {
			sb.WriteString(styles.ProgressBg.Render(ch))
		}
	}
	return sb.String()
}

func centerLine(s string, width int) string {
	sw := lipgloss.Width(s)
	pad := max(0, (width-sw)/2)
	return strings.Repeat(" ", pad) + s
}
