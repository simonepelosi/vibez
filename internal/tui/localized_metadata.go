package tui

import (
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/tui/views"
)

// MusicKit may re-fetch playback items in the storefront default language.
// Keep the localized catalog labels without replacing IDs or playback state.
func (m *Model) localizePlaybackMetadata(s *player.State) {
	if m.cfg.AppleLanguage == "" || s.Track == nil {
		return
	}
	id := views.PlaybackID(*s.Track)
	if id == "" {
		return
	}
	for _, queued := range m.queueTracks {
		if views.PlaybackID(queued) != id && !(queued.CatalogID != "" && (queued.CatalogID == s.Track.ID || queued.CatalogID == s.Track.CatalogID)) && !(s.Track.CatalogID != "" && queued.ID == s.Track.CatalogID) {
			continue
		}
		track := *s.Track
		if queued.Title != "" {
			track.Title = queued.Title
		}
		if queued.Artist != "" {
			track.Artist = queued.Artist
		}
		if queued.Album != "" {
			track.Album = queued.Album
		}
		s.Track = &track
		return
	}
}
