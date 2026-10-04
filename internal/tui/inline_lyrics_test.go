package tui

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/lyrics"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

func TestInlineLyricsKeepCoverOnLeftAndPanelsBelow(t *testing.T) {
	cfg := testCfg()
	cfg.InlineLyrics = true
	m := New(cfg, &mockProvider{}, nil, Options{})
	m.width, m.height = 140, 45
	m.introStep = introDone
	m.artMode = true
	m.supportsArtColor = func() bool { return true }
	m.artwork.url = "cover"
	m.artwork.img = image.NewNRGBA(image.Rect(0, 0, 512, 512))
	m.playerState.Track = &provider.Track{ID: "song", Title: "七里香", ArtworkURL: "cover"}
	m.playerState.Position = 2 * time.Second
	m.lastLyricsTrackID = "song"
	m.Update(lyricsResultMsg{trackID: "song", result: &lyrics.Result{Synced: true, Lines: []lyrics.Line{{Start: time.Second, Text: "测试歌词"}}}})
	rows := m.nowPlayingLines(136, m.nowPlayingHeight())
	cw, _ := m.coverPanelSize(136, m.nowPlayingHeight())
	coverSeen, lyricsSeen := false, false
	for _, row := range rows {
		text := ansi.Strip(row)
		if strings.ContainsRune(text, '▀') {
			coverSeen = true
		}
		if pos := strings.Index(text, "测试歌词"); pos >= 0 {
			lyricsSeen = true
			if ansi.StringWidth(text[:pos]) < cw+3 {
				t.Fatal("lyrics overlap album art")
			}
		}
		if ansi.StringWidth(row) > 136 {
			t.Fatal("row exceeds frame")
		}
	}
	if !coverSeen || !lyricsSeen {
		t.Fatal("missing cover or lyrics")
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Queue") || !strings.Contains(frame, "Vibe") {
		t.Fatal("bottom panels were replaced")
	}
	m.handleNormalKey(tea.KeyPressMsg{Text: "y"}, "y")
	if m.inlineLyrics {
		t.Fatal("y did not hide inline lyrics")
	}
}

func TestFailedLyricsRetryWithoutHidingPanel(t *testing.T) {
	m := newModel(nil)
	m.cfg.InlineLyrics = true
	m.inlineLyrics = true
	m.playerState.Track = &provider.Track{ID: "song", Title: "测试"}
	m.lyricsP.m.SetLyrics(nil, fmt.Errorf("lrclib: status 503"))
	if m.handleNormalKey(tea.KeyPressMsg{Text: "y"}, "y") == nil || !m.inlineLyrics {
		t.Fatal("retry hides panel or does not fetch")
	}
}

func TestLyricsFetchAfterOptimisticSearchSelection(t *testing.T) {
	m := newModel(nil)
	m.cfg.InlineLyrics = true
	m.inlineLyrics = true
	track := &provider.Track{ID: "new-song", Title: "同名歌曲"}
	m.playerState.Track = track // search/library displays selection before SDK confirms it
	m.lastLyricsTrackID = "old-song"
	m.Update(playerStateMsg(player.State{Track: track, Playing: true, Position: time.Second}))
	if m.lastLyricsTrackID != "new-song" || !strings.Contains(ansi.Strip(strings.Join(m.lyricsP.m.InlineLines(80, 10), "\n")), "Loading lyrics") {
		t.Fatal("optimistic selection suppresses lyrics fetch")
	}
}
