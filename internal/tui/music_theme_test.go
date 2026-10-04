package tui

import (
	"testing"

	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

func TestMusicThemeFollowsGenreAndRestoresCustomBase(t *testing.T) {
	defer styles.Apply(styles.DefaultTheme())
	base := styles.DefaultTheme()
	base.Primary = "#123456"
	cfg := testCfg()
	cfg.GenreTheme = true
	m := New(cfg, &mockProvider{}, nil, Options{BaseTheme: &base})
	for _, tt := range []struct {
		genres []string
		want   string
	}{
		{[]string{"Music", "Mandopop"}, "default"},
		{[]string{"Hip-Hop/Rap"}, "gruvbox"},
		{[]string{"Electronic"}, "dracula"},
		{[]string{"Classical"}, "nord"},
		{nil, "base"},
	} {
		m.syncMusicTheme(&provider.Track{Genres: tt.genres})
		if m.musicTheme != tt.want {
			t.Fatalf("%v: got %s want %s", tt.genres, m.musicTheme, tt.want)
		}
	}
	if styles.ColorHex(styles.ColorPrimary) != base.Primary {
		t.Fatal("custom startup theme not restored")
	}
	m.queueTracks = []provider.Track{{ID: "1", Genres: []string{"Jazz"}}}
	m.syncMusicTheme(&provider.Track{ID: "1"})
	if m.musicTheme != "gruvbox" {
		t.Fatal("queue genre fallback failed")
	}
}
func TestGenreThemeIsOptIn(t *testing.T) {
	m := newModel(nil)
	m.syncMusicTheme(&provider.Track{Genres: []string{"Rock"}})
	if m.musicTheme != "base" {
		t.Fatal("changed theme without opting in")
	}
}
