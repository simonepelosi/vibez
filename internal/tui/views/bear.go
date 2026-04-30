package views

// Animated bear mascot.
// Two animation modes selected by the caller:
//   playing=false → sleeping bear (z's floating up, slow cycle)
//   playing=true  → dancing bear (♪ ♫ notes, faster cycle)

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// BearLines is the number of lines RenderBear always returns.
const BearLines = 3

type bearFrame struct {
	above string // annotation above the bear (empty → blank line)
	expr  string // bear kaomoji
	below string // annotation below the bear (empty → blank line)
}

var sleepFrames = []bearFrame{
	{above: "z", expr: "ʕ-ᴥ-ʔ"},
	{expr: "ʕ˘ᴥ˘ʔ"},
	{above: "zZ", expr: "ʕ-ᴥ-ʔ"},
	{above: "ZZZ", expr: "ʕ˘ᴥ˘ʔ"},
}

var danceFrames = []bearFrame{
	{above: "♪", expr: "ʕ·ᴥ·ʔ"},
	{expr: "ʕ•̀ω•́ʔ✧", below: "♫"},
	{above: "♫", expr: "ʕง•ᴥ•ʔง"},
	{expr: "ʕっ•ᴥ•ʔっ", below: "♪"},
	{above: "♪", expr: "ʕ•ᴥ•ʔゝ☆"},
}

// RenderBearLine returns a single animated line: bear kaomoji + note/z + status text.
// Used in the Now Playing section.
func RenderBearLine(step int, playing bool) string {
	if playing {
		f := danceFrames[(step/6)%len(danceFrames)]
		note := "♪"
		if f.above != "" {
			note = f.above
		} else if f.below != "" {
			note = f.below
		}
		status := styles.VibingStatus.Render("vibing...")
		return styles.BearStyle.Render(f.expr) + styles.NoteStyle.Render(note) + " " + status
	}
	f := sleepFrames[(step/12)%len(sleepFrames)]
	zz := ""
	if f.above != "" {
		zz = styles.SleepStyle.Render(f.above) + " "
	}
	return zz + styles.BearStyle.Render(f.expr) + " " + styles.SleepStyle.Render("zZz...")
}

// BearExpr returns just the styled bear kaomoji for the current animation step.
// Used in the header and status bar.
func BearExpr(step int, playing bool) string {
	if playing {
		f := danceFrames[(step/6)%len(danceFrames)]
		return styles.BearStyle.Render(f.expr)
	}
	f := sleepFrames[(step/12)%len(sleepFrames)]
	return styles.BearStyle.Render(f.expr)
}

// RenderBear returns exactly BearLines centred lines.
// playing=false → sleeping bear (slow z-cycle); playing=true → dancing bear.
func RenderBear(step int, playing bool, width int) string {
	var f bearFrame
	var annot lipgloss.Style

	if playing {
		f = danceFrames[(step/6)%len(danceFrames)]
		annot = styles.NoteStyle
	} else {
		f = sleepFrames[(step/12)%len(sleepFrames)] // slower: ≈960 ms/frame
		annot = styles.SleepStyle
	}

	above := ""
	if f.above != "" {
		above = centerLine(annot.Render(f.above), width)
	}
	bear := centerLine(styles.BearStyle.Render(f.expr), width)
	below := ""
	if f.below != "" {
		below = centerLine(annot.Render(f.below), width)
	}

	return strings.Join([]string{above, bear, below}, "\n")
}
