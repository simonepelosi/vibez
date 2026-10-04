package tui

import (
	"strings"

	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// Use the existing built-in palettes; classification follows catalog metadata.
func musicThemeName(genres []string) string {
	for _, raw := range genres {
		g := strings.ToLower(raw)
		switch {
		case strings.Contains(g, "rock"), strings.Contains(g, "metal"), strings.Contains(g, "electronic"), strings.Contains(g, "dance"), strings.Contains(g, "摇滚"), strings.Contains(g, "电子"):
			return "dracula"
		case strings.Contains(g, "hip-hop"), strings.Contains(g, "hip hop"), strings.Contains(g, "rap"), strings.Contains(g, "r&b"), strings.Contains(g, "soul"), strings.Contains(g, "jazz"), strings.Contains(g, "嘻哈"), strings.Contains(g, "爵士"):
			return "gruvbox"
		case strings.Contains(g, "classical"), strings.Contains(g, "ambient"), strings.Contains(g, "instrumental"), strings.Contains(g, "古典"), strings.Contains(g, "纯音乐"):
			return "nord"
		case strings.Contains(g, "pop"), strings.Contains(g, "流行"):
			return "default"
		}
	}
	return ""
}

func (m *Model) syncMusicTheme(track *provider.Track) {
	if m.cfg.CoverTheme {
		if track == nil || track.ArtworkURL == "" || track.ArtworkURL != m.coverThemeURL {
			if m.musicTheme != "base" {
				styles.Apply(m.baseTheme)
				m.musicTheme = "base"
			}
			m.coverThemeURL = ""
		}
		return
	}
	if track == nil || !m.cfg.GenreTheme {
		return
	}
	genres := track.Genres
	// Some MusicKit playback items omit genres: reuse the catalog queue entry.
	if len(genres) == 0 {
		for _, queued := range m.queueTracks {
			if (track.ID != "" && queued.ID == track.ID) || (queued.Title == track.Title && queued.Artist == track.Artist) {
				genres = queued.Genres
				break
			}
		}
	}
	name := musicThemeName(genres)
	if name == "" {
		name = "base"
	}
	if name == m.musicTheme {
		return
	}
	theme := m.baseTheme
	if name != "base" {
		theme, _ = styles.LoadTheme(name, "")
	}
	styles.Apply(theme)
	m.musicTheme = name
}

func (m *Model) musicMascot() string {
	if !m.playerState.Playing {
		return ""
	}
	switch m.musicTheme {
	case "dracula":
		return "(⌐■_■) ♫"
	case "gruvbox":
		return "(•‿•) ♪"
	case "nord":
		return "(˘‿˘) ♩"
	default:
		return ""
	}
}
