package tui

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// --- wrapFit ---

func TestWrapFit_SingleLineWhenAllFit(t *testing.T) {
	got := wrapFit([]string{"aaa", "bbb", "ccc"}, "-", 20)
	if len(got) != 1 || got[0] != "aaa-bbb-ccc" {
		t.Errorf("wrapFit() = %q, want one line %q", got, "aaa-bbb-ccc")
	}
}

func TestWrapFit_WrapsInsteadOfDropping(t *testing.T) {
	got := wrapFit([]string{"aaa", "bbb", "ccc"}, "-", 8)
	want := []string{"aaa-bbb", "ccc"}
	if len(got) != len(want) {
		t.Fatalf("wrapFit() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestWrapFit_LosesNothing is the core guarantee of the wrap approach: every
// part survives somewhere, at any width.
func TestWrapFit_LosesNothing(t *testing.T) {
	parts := []string{"first", "second", "third", "fourth", "fifth"}
	for w := 1; w <= 60; w++ {
		joined := strings.Join(wrapFit(parts, " · ", w), "\n")
		for _, p := range parts {
			if !strings.Contains(joined, p) {
				t.Errorf("w=%d dropped %q (got %q)", w, p, joined)
			}
		}
	}
}

func TestWrapFit_LinesRespectWidth(t *testing.T) {
	parts := []string{"first", "second", "third", "fourth", "fifth"}
	for w := 8; w <= 60; w++ {
		for i, line := range wrapFit(parts, " · ", w) {
			// A lone part wider than w cannot be split here; padRight clips it.
			if lipgloss.Width(line) > w && !isSinglePart(line, parts) {
				t.Errorf("w=%d line %d is %d wide: %q", w, i, lipgloss.Width(line), line)
			}
		}
	}
}

func isSinglePart(line string, parts []string) bool {
	return slices.Contains(parts, line)
}

func TestWrapFit_PreservesStyledParts(t *testing.T) {
	styled := lipgloss.NewStyle().Bold(true).Render("bold")
	got := strings.Join(wrapFit([]string{styled, "plain"}, " ", 6), "\n")
	if !strings.Contains(got, styled) {
		t.Errorf("wrapFit() cut the styled part: %q", got)
	}
}

func TestWrapFit_EmptyAndZeroWidth(t *testing.T) {
	if got := wrapFit(nil, "-", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("wrapFit(nil) = %q, want one empty line", got)
	}
	if got := wrapFit([]string{"a"}, "-", 0); len(got) != 1 || got[0] != "" {
		t.Errorf("wrapFit(w=0) = %q, want one empty line", got)
	}
}

// --- padRight ---

func TestPadRight_ClipsOverlongInput(t *testing.T) {
	got := padRight("abcdefghij", 5)
	if gw := lipgloss.Width(got); gw != 5 {
		t.Errorf("padRight() width = %d, want 5 (%q)", gw, got)
	}
}

func TestPadRight_ClipDoesNotBreakANSI(t *testing.T) {
	styled := lipgloss.NewStyle().Bold(true).Render("abcdefghij")
	got := padRight(styled, 5)
	if gw := lipgloss.Width(got); gw != 5 {
		t.Errorf("padRight() visual width = %d, want 5", gw)
	}
	// An ANSI-aware clip keeps the reset sequence so styling cannot bleed on.
	if strings.Contains(styled, "\x1b[") && !strings.Contains(got, "\x1b[m") {
		t.Errorf("padRight() clipped away the reset sequence: %q", got)
	}
}

func TestPadRight_NonPositiveWidth(t *testing.T) {
	if got := padRight("abc", 0); got != "" {
		t.Errorf("padRight(w=0) = %q, want empty", got)
	}
	if got := padRight("abc", -3); got != "" {
		t.Errorf("padRight(w=-3) = %q, want empty", got)
	}
}

// --- border integrity ---

// TestView_StatusLinesRespectBorder is the regression guard for the status
// hints ("v vibe", "R radio") spilling past the right border and breaking the
// vertical rule. Every rendered row must be exactly m.width columns wide.
func TestView_StatusLinesRespectBorder(t *testing.T) {
	for _, w := range []int{40, 60, 72, 80, 100, 120, 200} {
		m := newModel(nil)
		m.width = w
		m.height = 30
		m.introStep = introDone

		for line := range strings.SplitSeq(m.View().Content, "\n") {
			if line == "" {
				continue
			}
			if lw := lipgloss.Width(line); lw != w {
				t.Errorf("width=%d: row is %d columns wide, want %d: %q", w, lw, w, line)
				break
			}
		}
	}
}

// TestView_HeightMatchesTerminal guards the panelHeight accounting: wrapping
// hints onto extra rows must come out of the panel area, not out of the frame.
func TestView_HeightMatchesTerminal(t *testing.T) {
	for _, w := range []int{60, 80, 100, 140} {
		m := newModel(nil)
		m.width = w
		m.height = 40 // tall enough that panelHeight is not clamped to its floor
		m.introStep = introDone

		got := len(strings.Split(m.View().Content, "\n"))
		if got != m.height {
			t.Errorf("width=%d: View() rendered %d rows, want %d", w, got, m.height)
		}
	}
}

// TestStatusLines_NothingHidden checks that no hint is dropped at any width,
// including with radio and discovery active (the longest variants).
func TestStatusLines_NothingHidden(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 120} {
		m := newModel(nil)
		m.width = w
		m.height = 40
		m.radio.enabled = true

		joined := strings.Join(m.statusLines(w-4), "\n")
		for _, hint := range []string{"command", "vibe", "radio", "quit", "shuffle"} {
			if !containsAny(joined, strings.Split(hint, "|")) {
				t.Errorf("width=%d: hint %q missing from status lines", w, hint)
			}
		}
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
