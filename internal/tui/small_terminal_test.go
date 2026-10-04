package tui

import (
	"github.com/charmbracelet/x/ansi"
	"testing"
)

func TestResizeToTinyTerminalDoesNotPanic(t *testing.T) {
	m := newModel(nil)
	m.introStep = introDone
	for w := range 12 {
		for h := range 8 {
			m.width, m.height = w, h
			v := m.View()
			if ansi.StringWidth(v.Content) > w {
				t.Fatal("tiny frame exceeds width")
			}
		}
	}
}
