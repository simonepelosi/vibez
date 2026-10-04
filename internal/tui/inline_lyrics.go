package tui

import "strings"

func (m *Model) coverPanelSize(w, h int) (int, int) {
	if !m.inlineLyrics {
		return w, h
	}
	if w >= 70 {
		return (w - 3) * 45 / 100, h
	}
	return w, max(8, h/2)
}
func (m *Model) coverAndLyricsLines(w, h int) []string {
	cw, ch := m.coverPanelSize(w, h)
	var cover []string
	if m.artModeActive() {
		cover = m.nowPlayingArtLines(cw, ch)
	} else {
		cover = m.nowPlayingTextLines(cw, ch)
	}
	if w < 70 {
		lines := append(cover, m.lyricsP.m.InlineLines(w, max(0, h-ch))...)
		return toLines(strings.Join(lines, "\n"), h)
	}
	rw := w - cw - 3
	lyrics := m.lyricsP.m.InlineLines(rw, h)
	lines := make([]string, h)
	for i := range h {
		lines[i] = padRight(safeIdx(cover, i), cw) + "   " + padRight(safeIdx(lyrics, i), rw)
	}
	return lines
}
