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

func TestClean_MalformedUTF8CloakingANSI(t *testing.T) {
	if got := Clean("bad\xff\x1b[31mred\x1b[0m"); got != "badred" {
		t.Fatalf("Clean(invalid UTF-8 before CSI) = %q, want %q", got, "badred")
	}
	if got := Clean("\x1b]0;\xfftitle\x07visible"); got != "visible" {
		t.Fatalf("Clean(invalid UTF-8 inside OSC) = %q, want %q", got, "visible")
	}
	if got := Clean("safe\x1b\xff[31mevil\x1b[0m"); got == "" || got[0] == '\x1b' {
		t.Fatalf("Clean(broken CSI) unexpected output: %q", got)
	}
}

func TestClean_CRAndCombiningMarksDoS(t *testing.T) {
	if got := Clean("first\rsecond\r\nthird"); got != "first second third" {
		t.Fatalf("Clean(CR/CRLF) = %q, want %q", got, "first second third")
	}
	if got := CleanLines("first\rsecond\r\nthird"); got != "first\nsecond\nthird" {
		t.Fatalf("CleanLines(CR/CRLF) = %q, want %q", got, "first\nsecond\nthird")
	}
	const comb = "\u0301" // 2 bytes in UTF-8
	zalgo := make([]byte, 0, 1+30*len(comb))
	zalgo = append(zalgo, 'a')
	for range 30 {
		zalgo = append(zalgo, comb...)
	}
	want := "a\u0301\u0301\u0301\u0301"
	if got := Clean(string(zalgo)); got != want {
		t.Fatalf("Clean(zalgo) len=%d (%q), want len=%d (%q)", len(got), got, len(want), want)
	}
}
