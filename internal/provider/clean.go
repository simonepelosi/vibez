package provider

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Clean makes text from outside vibez - Apple Music metadata, file tags and
// names, lyrics - safe to print in a terminal. Terminal escape sequences,
// invalid UTF-8 bytes, control characters (tabs and newlines become a space),
// and Unicode bidirectional overrides/isolates are stripped.
func Clean(s string) string { return clean(s, false) }

// CleanLines is Clean for text that is meant to span lines, such as plain
// lyrics: newlines are kept.
func CleanLines(s string) string { return clean(s, true) }

func clean(s string, keepNewlines bool) string {
	s = ansi.Strip(s)
	isSpace := func(r rune) bool {
		return r == '\t' || r == '\u2028' || r == '\u2029' || (r == '\n' && !keepNewlines)
	}
	dirty := !utf8.ValidString(s)
	if !dirty {
		for _, r := range s {
			if isSpace(r) || (drop(r) && (r != '\n' || !keepNewlines)) {
				dirty = true
				break
			}
		}
	}
	if !dirty {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		switch {
		case isSpace(r):
			b.WriteByte(' ')
		case r == '\n' && keepNewlines:
			b.WriteByte('\n')
		case drop(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CleanAll applies Clean to every element of ss in place and returns it.
func CleanAll(ss []string) []string {
	for i := range ss {
		ss[i] = Clean(ss[i])
	}
	return ss
}

func drop(r rune) bool {
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.IsControl(r) {
		return true
	}
	switch r {
	case '\u061c', '\u200e', '\u200f', // ALM, LRM, RLM
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // embeddings and overrides
		'\u2066', '\u2067', '\u2068', '\u2069': // isolates
		return true
	case '\u200c', '\u200d': // ZWNJ, ZWJ
		return false
	}
	return !unicode.IsPrint(r) && r != ' '
}
