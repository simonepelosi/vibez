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

// maxCombiningMarks caps consecutive Unicode combining marks (Mn/Mc/Me) after a
// base character so malicious Zalgo-style metadata cannot degrade terminal
// width calculation or rendering.
const maxCombiningMarks = 4

func scrubInvalidUTF8(s string) string {
	if utf8.ValidString(s) {
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
		b.WriteRune(r)
	}
	return b.String()
}

func clean(s string, keepNewlines bool) string {
	s = ansi.Strip(s)
	if !utf8.ValidString(s) {
		// Re-run ANSI stripping after dropping invalid UTF-8 bytes so malformed
		// byte sequences inside or around an ESC sequence cannot smuggle CSI/OSC.
		s = ansi.Strip(scrubInvalidUTF8(s))
	}
	isSpace := func(r rune) bool {
		return r == '\t' || r == '\u2028' || r == '\u2029' || (r == '\n' && !keepNewlines)
	}
	dirty := !utf8.ValidString(s)
	if !dirty {
		combRun := 0
		for _, r := range s {
			if r == '\r' || isSpace(r) || (drop(r) && (r != '\n' || !keepNewlines)) {
				dirty = true
				break
			}
			if unicode.IsMark(r) {
				combRun++
				if combRun > maxCombiningMarks {
					dirty = true
					break
				}
			} else {
				combRun = 0
			}
		}
	}
	if !dirty {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	combRun := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if r == '\r' {
			if i < len(s) && s[i] == '\n' {
				i++
			}
			if keepNewlines {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
			combRun = 0
			continue
		}
		switch {
		case isSpace(r):
			b.WriteByte(' ')
			combRun = 0
		case r == '\n' && keepNewlines:
			b.WriteByte('\n')
			combRun = 0
		case drop(r):
		case unicode.IsMark(r):
			combRun++
			if combRun <= maxCombiningMarks {
				b.WriteRune(r)
			}
		default:
			combRun = 0
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
