package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommandInputVisibleAndUnicode(t *testing.T) {
	m := newModel(nil)
	m.mode = modeCommand
	m.introStep = introDone
	m.width, m.height = 90, 40
	m.handleKey(tea.KeyPressMsg{Text: "save 夜晚"})
	m.Update(tea.PasteMsg{Content: "歌单\n"})
	if m.cmdBuf != "save 夜晚歌单" {
		t.Fatalf("input lost: %q", m.cmdBuf)
	}
	m.handleCommandKey("backspace")
	if m.cmdBuf != "save 夜晚歌" || !utf8.ValidString(m.cmdBuf) {
		t.Fatal("backspace corrupts Unicode")
	}
	if !strings.Contains(ansi.Strip(m.commandLines(86, 8)[0]), m.cmdBuf) {
		t.Fatal("panel hides command input")
	}
	v := m.View()
	if v.Cursor == nil || v.Cursor.Y != m.nowPlayingHeight()+4 {
		t.Fatal("IME cursor missing or misplaced")
	}
	m.cmdBuf = strings.Repeat("中文", 80)
	if ansi.StringWidth(m.commandLines(86, 8)[0]) > 86 || m.View().Cursor.X >= m.width-2 {
		t.Fatal("long input escapes frame")
	}
}
