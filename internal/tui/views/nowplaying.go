package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

type NowPlayingModel struct {
	state    *player.State
	width    int
	height   int
	glowStep int
}

func NewNowPlaying(state *player.State) *NowPlayingModel {
	return &NowPlayingModel{state: state}
}

func (m *NowPlayingModel) SetState(s *player.State) { m.state = s }
func (m *NowPlayingModel) SetGlowStep(step int)     { m.glowStep = step }

func (m *NowPlayingModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *NowPlayingModel) Update(_ tea.KeyMsg) {}

func (m *NowPlayingModel) View() string {
	if m.state == nil || m.state.Track == nil {
		return ""
	}

	t := m.state.Track
	var sb strings.Builder

	// Vertical centering — place content roughly in the middle.
	topPad := max(0, (m.height-8)/2)
	sb.WriteString(strings.Repeat("\n", topPad))

	// Track info — title sweeps a bright spot left→right when playing.
	var renderedTitle string
	if m.state.Playing {
		renderedTitle = renderGlowTitle(t.Title, m.glowStep)
	} else {
		renderedTitle = styles.NowPlayingTitle.Render(t.Title)
	}
	sb.WriteString(centerLine(renderedTitle, m.width))
	sb.WriteString("\n")
	sb.WriteString(centerLine(styles.NowPlayingArtist.Render(t.Artist), m.width))
	sb.WriteString("\n")
	sb.WriteString(centerLine(styles.NowPlayingAlbum.Render(t.Album), m.width))
	sb.WriteString("\n\n")

	// Status + time — the only playback indicator.
	icon := "⏸"
	statusStyle := styles.Paused
	if m.state.Playing {
		icon = "▶"
		statusStyle = styles.Playing
	}
	elapsed := FormatDuration(m.state.Position)
	total := FormatDuration(t.Duration)
	status := statusStyle.Render(fmt.Sprintf("%s  %s / %s", icon, elapsed, total))
	sb.WriteString(centerLine(status, m.width))

	return sb.String()
}

// renderGlowTitle renders each rune with a colour based on how far the
// "bright spot" has swept past it. The spot starts before the first char,
// sweeps right, exits after the last char, then the cycle restarts.
func renderGlowTitle(title string, glowStep int) string {
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
		sb.WriteString(lipgloss.NewStyle().Foreground(color).Bold(true).Render(string(r)))
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

func centerLine(s string, width int) string {
	sw := lipglossWidth(s)
	pad := max(0, (width-sw)/2)
	return strings.Repeat(" ", pad) + s
}

func centerText(text string, w, h int) string {
	pad := max(0, h/2)
	return strings.Repeat("\n", pad) + centerLine(styles.QueueItemMuted.Render(text), w)
}

// lipglossWidth measures visible rune width, stripping ANSI codes.
func lipglossWidth(s string) int {
	inEscape := false
	width := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		width++
	}
	return width
}
