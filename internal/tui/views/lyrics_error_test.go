package views

import (
	"errors"
	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/lyrics"
	"strings"
	"testing"
)

func TestLyricsErrorsDistinguishUnavailableService(t *testing.T) {
	m := NewLyrics()
	m.SetLyrics(nil, errors.New("lrclib: status 503"))
	if !strings.Contains(ansi.Strip(strings.Join(m.InlineLines(80, 10), "\n")), "request failed") {
		t.Fatal("network failure misreported as missing lyrics")
	}
	m.SetLyrics(nil, lyrics.ErrNotFound)
	if !strings.Contains(ansi.Strip(strings.Join(m.InlineLines(80, 10), "\n")), "no matching result") {
		t.Fatal("not-found message missing")
	}
}
