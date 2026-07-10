package views

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAboutModel(t *testing.T) {
	m := NewAbout()
	m.SetSize(80, 20)

	// Test initial View
	view := m.View()
	t.Logf("view: %q", view)
	plain := stripANSI(view)
	if !strings.Contains(plain, "vibez") {
		t.Error("expected view to contain 'vibez'")
	}
	if !strings.Contains(plain, "made with ❤️ by simonepelosi") {
		t.Error("expected view to contain 'made with ❤️ by simonepelosi'")
	}
	if !strings.Contains(plain, "https://ko-fi.com/pelpsi") {
		t.Error("expected view to contain 'https://ko-fi.com/pelpsi'")
	}

	// Test Update with random key
	cmd := m.Update(tea.KeyPressMsg{Text: "x"})
	if cmd != nil {
		t.Error("expected nil command for unhandled key")
	}

	// Test Update with enter
	cmd = m.Update(tea.KeyPressMsg{Text: "enter"})
	if cmd == nil {
		t.Fatal("expected non-nil command for enter key")
	}
	_ = cmd() // run it

	// Test View after open link status change
	viewAfter := m.View()
	plainAfter := stripANSI(viewAfter)
	if !strings.Contains(plainAfter, "Opening donation link") {
		t.Error("expected view to indicate that the donation link is opening")
	}

	// Test Update with d
	m = NewAbout()
	cmd = m.Update(tea.KeyPressMsg{Text: "d"})
	if cmd == nil {
		t.Fatal("expected non-nil command for d key")
	}
	_ = cmd()
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
