package provider

import (
	"strings"
	"unicode"
)

// Clean makes text from outside vibez - Apple Music metadata, file tags and
// names, lyrics - safe to print in a terminal. Control characters, which
// include the ESC that starts an escape sequence, are dropped (tab and newline
// become a space), and so are the Unicode bidirectional overrides that reorder
// what follows them on screen. A track tag can otherwise carry terminal
// commands, or make one string read as another.
func Clean(s string) string { return clean(s, false) }

// CleanLines is Clean for text that is meant to span lines, such as plain
// lyrics: newlines are kept.
func CleanLines(s string) string { return clean(s, true) }

func clean(s string, keepNewlines bool) string {
	isSpace := func(r rune) bool { return r == '\t' || (r == '\n' && !keepNewlines) }
	dirty := false
	for _, r := range s {
		if isSpace(r) || (drop(r) && (r != '\n' || !keepNewlines)) {
			dirty = true
			break
		}
	}
	if !dirty {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
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
	if unicode.IsControl(r) { // C0, DEL and C1 (which includes CSI, U+009B)
		return true
	}
	switch r {
	case '\u200e', '\u200f', // LRM, RLM
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // embeddings and overrides
		'\u2066', '\u2067', '\u2068', '\u2069': // isolates
		return true
	}
	return false
}
