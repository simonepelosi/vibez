package views

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BearLines is the number of lines RenderBear returns (always 3:
// note-or-blank / bear / note-or-blank).
const BearLines = 3

type bearFrame struct {
	above string // note above the bear (empty → blank line)
	expr  string // bear kaomoji
	below string // note below the bear (empty → blank line)
}

var bearFrames = []bearFrame{
	{above: "♪", expr: "ʕ·ᴥ·ʔ"},
	{expr: "ʕ•̀ω•́ʔ✧", below: "♫"},
	{above: "♫", expr: "ʕง•ᴥ•ʔง"},
	{expr: "ʕっ•ᴥ•ʔっ", below: "♪"},
	{above: "♪", expr: "ʕ•ᴥ•ʔゝ☆"},
}

var (
	bearStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#C4A265"))
	noteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
)

// RenderBear returns exactly BearLines lines of centred animated bear art.
// step is the model's glowStep (ticks at ~80 ms); frame advances every
// 6 ticks (≈480 ms) for a gentle, not frantic, animation.
func RenderBear(step, width int) string {
	f := bearFrames[(step/6)%len(bearFrames)]

	above := ""
	if f.above != "" {
		above = centerLine(noteStyle.Render(f.above), width)
	}
	bear := centerLine(bearStyle.Render(f.expr), width)
	below := ""
	if f.below != "" {
		below = centerLine(noteStyle.Render(f.below), width)
	}

	return strings.Join([]string{above, bear, below}, "\n")
}
