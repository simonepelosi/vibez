package views

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/lyrics"
)

func TestInlineDuetAlignsBothVoicesAndCentersChorus(t *testing.T) {
	m := NewLyrics()
	m.SetLyrics(&lyrics.Result{Synced: true, Lines: []lyrics.Line{
		{Start: time.Second, Speaker: "甲", Text: "第一句"},
		{Start: time.Second, Speaker: "乙", Text: "第二句"},
		{Start: 3 * time.Second, Speaker: "合唱", Text: "一起唱"},
	}}, nil)
	m.SetPosition(time.Second)
	rows := m.InlineLines(60, 10)
	if !strings.HasPrefix(strings.TrimSpace(ansi.Strip(rows[5])), "甲") {
		t.Fatal("first singer not aligned left")
	}
	if !strings.HasSuffix(ansi.Strip(rows[6]), "第二句") || !strings.HasPrefix(ansi.Strip(rows[6]), " ") {
		t.Fatal("second singer not aligned right")
	}
	if !strings.HasPrefix(ansi.Strip(rows[8]), " ") {
		t.Fatal("chorus not centered")
	}
	// Both active lines use the same highlighted style, including bold.
	for _, row := range rows[5:7] {
		if !strings.Contains(row, "\x1b[1;") && !strings.Contains(row, "\x1b[1m") {
			t.Fatalf("simultaneous singer not highlighted: %q", row)
		}
	}
	for _, row := range rows {
		if ansi.StringWidth(row) > 60 {
			t.Fatal("lyric exceeds panel")
		}
	}
}
