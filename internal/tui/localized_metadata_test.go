package tui

import (
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

func TestLocalizedQueueMetadataSurvivesMusicKitTick(t *testing.T) {
	m := newModel(nil)
	m.cfg.AppleLanguage = "zh-Hans-CN"
	m.queueTracks = []provider.Track{{ID: "i.local", CatalogID: "song", Title: "七里香", Artist: "周杰伦", Album: "七里香"}}
	raw := &provider.Track{ID: "song", Title: "Qi-Li-Xiang", Artist: "Jay Chou"}
	s := player.State{Track: raw, Position: time.Minute, Playing: true}
	m.localizePlaybackMetadata(&s)
	if s.Track.Title != "七里香" || s.Track.Artist != "周杰伦" || s.Track.ID != "song" || s.Position != time.Minute {
		t.Fatal("localized labels or playback identity lost")
	}
	if raw.Title != "Qi-Li-Xiang" {
		t.Fatal("mutated shared player track")
	}
}
