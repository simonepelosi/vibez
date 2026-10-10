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
	for _, word := range []string{"ko-fi.com", "donat", "Opening"} {
		if strings.Contains(plain, word) {
			t.Fatalf("unwanted donation UI: %s", word)
		}
	}
	for _, key := range []string{"x", "enter", "d"} {
		if m.Update(tea.KeyPressMsg{Text: key}) != nil {
			t.Fatalf("about key %s must not launch a browser", key)
		}
	}

}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
