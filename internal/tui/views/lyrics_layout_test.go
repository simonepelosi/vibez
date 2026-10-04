package views

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/lyrics"
	"strings"
	"testing"
	"time"
)

func TestLyricsWrapSpaceAndAnimate(t *testing.T) {
	m := NewLyrics()
	var lines []lyrics.Line
	for i := range 12 {
		lines = append(lines, lyrics.Line{Start: time.Duration(i) * time.Second, Text: strings.Repeat("测试", 15)})
	}
	m.SetLyrics(&lyrics.Result{Synced: true, Lines: lines}, nil)
	m.SetPosition(3 * time.Second)
	rows := m.InlineLines(35, 15)
	if strings.Contains(strings.Join(rows, ""), "…") {
		t.Fatal("lyrics truncated instead of wrapped")
	}
	for _, row := range rows {
		if ansi.StringWidth(row) > 35 {
			t.Fatal("wrapped lyric escapes viewport")
		}
	}
	before := m.viewportOffset
	m.SetPosition(4 * time.Second)
	m.InlineLines(35, 15)
	if m.viewportOffset != before || m.viewportTarget <= before {
		t.Fatal("no scroll transition queued")
	}
	m.AdvanceFrame()
	if m.viewportOffset != before+1 {
		t.Fatal("animation did not advance")
	}
	m.SetPosition(time.Second)
	m.InlineLines(35, 15)
	if m.currentIdx != 1 {
		t.Fatal("seek did not update active lyric")
	}
}
