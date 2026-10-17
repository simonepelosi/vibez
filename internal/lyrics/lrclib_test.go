package lyrics

import (
	"strings"
	"testing"
)

// Lyrics come from a community database and are printed in the terminal, so a
// line must never carry an escape sequence through.
func TestParseLRCStripsControlSequences(t *testing.T) {
	lines, err := parseLRC("[00:01.00] hello \x1b[2Jworld\x1b]0;pwned\x07\n[00:02.50] \u202edrawkcab")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, l := range lines {
		if strings.ContainsAny(l.Text, "\x1b\x07\u202e") {
			t.Errorf("line %q still carries a control or override character", l.Text)
		}
	}
	if lines[0].Text != "hello [2Jworld]0;pwned" {
		t.Errorf("line 0 = %q", lines[0].Text)
	}
}
