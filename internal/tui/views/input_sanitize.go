package views

import (
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

const (
	// MaxInputRunes bounds single-line search and command buffers so pasted payloads
	// cannot cause unbounded allocations or oversized API queries.
	MaxInputRunes = 256
	// MaxPasteBytes bounds raw clipboard bytes inspected before stripping ANSI/control codes.
	MaxPasteBytes = 4096
)

// isUnsafeInputRune rejects C0/C1 control codes, DEL, non-printable runes, and
// Unicode BiDi override/isolate characters that could spoof or corrupt terminal
// rendering or playlist names.
func isUnsafeInputRune(r rune) bool {
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return true
	}
	if !unicode.IsPrint(r) && r != ' ' {
		return true
	}
	switch {
	case r >= 0x202a && r <= 0x202e: // LRE, RLE, PDF, LRO, RLO
		return true
	case r >= 0x2066 && r <= 0x2069: // LRI, RLI, FSI, PDI
		return true
	case r == 0x200e || r == 0x200f || r == 0x061c: // LRM, RLM, ALM
		return true
	}
	return false
}

// SanitizeSingleLineInput strips terminal escape sequences, invalid UTF-8 bytes,
// control characters, and BiDi overrides, returning at most maxRunes runes.
// When collapseNewlines is true (paste), tabs and newlines are converted to single spaces.
func SanitizeSingleLineInput(s string, maxRunes int, collapseNewlines bool) []rune {
	if maxRunes <= 0 || s == "" {
		return nil
	}
	if len(s) > MaxPasteBytes {
		cut := MaxPasteBytes
		for range utf8.UTFMax - 1 {
			if cut <= 0 || utf8.RuneStart(s[cut]) {
				break
			}
			cut--
		}
		s = s[:cut]
	}
	s = ansi.Strip(s)
	out := make([]rune, 0, min(len(s), maxRunes))
	lastWasCollapsedSpace := false
	for i := 0; i < len(s) && len(out) < maxRunes; {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if collapseNewlines && (r == '\r' || r == '\n' || r == '\t' || r == '\v' || r == '\f' || r == '\u0085' || r == '\u2028' || r == '\u2029') {
			if len(out) > 0 && !lastWasCollapsedSpace && out[len(out)-1] != ' ' {
				out = append(out, ' ')
				lastWasCollapsedSpace = true
			}
			continue
		}
		if isUnsafeInputRune(r) {
			continue
		}
		if r != ' ' && unicode.IsSpace(r) {
			r = ' '
		}
		lastWasCollapsedSpace = false
		out = append(out, r)
	}
	if lastWasCollapsedSpace && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out
}
