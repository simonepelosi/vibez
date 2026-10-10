package provider

import "testing"

func TestClean(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Plain Title", "Plain Title"},
		{"日本語 – ünïcode ♪", "日本語 – ünïcode ♪"},
		{"", ""},
		{"a\x1b[31mred\x1b[0m", "a[31mred[0m"},
		{"clear\x1b[2J\x1b[H", "clear[2J[H"},
		{"title\x1b]0;pwned\x07", "title]0;pwned"},
		{"c1\u009b31m", "c131m"},
		{"two\nlines\tand tab", "two lines and tab"},
		{"nul\x00byte\x7f", "nulbyte"},
		{"evil\u202egnp.exe", "evilgnp.exe"},
		{"iso\u2066late\u2069d", "isolated"},
	}
	for _, c := range cases {
		if got := Clean(c.in); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanAll(t *testing.T) {
	got := CleanAll([]string{"Rock\x1b[1m", "Pop"})
	if got[0] != "Rock[1m" || got[1] != "Pop" {
		t.Errorf("CleanAll = %q", got)
	}
}

func TestCleanLinesKeepsNewlines(t *testing.T) {
	got := CleanLines("line one\x1b[31m\nline\ttwo\u202e\n")
	if want := "line one[31m\nline two\n"; got != want {
		t.Errorf("CleanLines = %q, want %q", got, want)
	}
}
