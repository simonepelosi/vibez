package provider

import (
	"testing"
	"unicode/utf8"
)

func TestClean(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Plain Title", "Plain Title"},
		{"日本語 – ünïcode ♪", "日本語 – ünïcode ♪"},
		{"", ""},
		{"a\x1b[31mred\x1b[0m", "ared"},
		{"clear\x1b[2J\x1b[H", "clear"},
		{"title\x1b]0;pwned\x07", "title"},
		{"link\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\", "linkclick"},
		{"c1\u009b31m", "c131m"},
		{"rawc1\x9b31mred", "rawc1red"},
		{"two\nlines\tand tab", "two lines and tab"},
		{"nul\x00byte\x7f", "nulbyte"},
		{"evil\u202egnp.exe\u061c", "evilgnp.exe"},
		{"iso\u2066late\u2069d", "isolated"},
		{"bad\xff\xfeutf8", "badutf8"},
		{"sep\u2028line\u2029para", "sep line para"},
		{"👩\u200d🎤", "👩\u200d🎤"},
	}
	for _, c := range cases {
		if got := Clean(c.in); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
		if !utf8.ValidString(Clean(c.in)) {
			t.Errorf("Clean(%q) returned invalid UTF-8", c.in)
		}
	}
}

func TestCleanAll(t *testing.T) {
	got := CleanAll([]string{"Rock\x1b[1m", "Pop"})
	if got[0] != "Rock" || got[1] != "Pop" {
		t.Errorf("CleanAll = %q", got)
	}
}

func TestCleanLinesKeepsNewlines(t *testing.T) {
	got := CleanLines("line one\x1b[31m\nline\ttwo\u202e\n")
	if want := "line one\nline two\n"; got != want {
		t.Errorf("CleanLines = %q, want %q", got, want)
	}
}

func FuzzClean(f *testing.F) {
	f.Add("\x1b[31mhi\x1b[0m\u202e\xff\x00\n")
	f.Add("\x1b]52;c;YWJj\x07normal")
	f.Fuzz(func(t *testing.T, s string) {
		got := Clean(s)
		if !utf8.ValidString(got) {
			t.Fatalf("Clean produced invalid UTF-8 for %q", s)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
				t.Fatalf("Clean leaked control rune %U", r)
			}
		}
	})
}
