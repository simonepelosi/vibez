package tui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

const testArtURL = "https://example.test/cover/300x300bb.jpg"

// newArtModel returns a model primed to render album art: a decoded four-colour
// test cover, matching artwork URL, a playing track, and truecolor enabled.
func newArtModel(t *testing.T) *Model {
	t.Helper()
	m := newModel(nil)
	m.width, m.height = 100, 30
	m.artColorOK = true

	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			var c color.RGBA
			switch {
			case x < 16 && y < 16:
				c = color.RGBA{R: 255, A: 255}
			case x >= 16 && y < 16:
				c = color.RGBA{G: 255, A: 255}
			case x < 16 && y >= 16:
				c = color.RGBA{B: 255, A: 255}
			default:
				c = color.RGBA{R: 255, G: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	m.artworkImg = img
	m.artworkURL = testArtURL
	m.playerState = player.State{
		Playing: true,
		Track: &provider.Track{
			Title:      "Test Title",
			Artist:     "Test Artist",
			Album:      "Test Album",
			ArtworkURL: testArtURL,
		},
	}
	return m
}

func TestNowPlayingLines_RendersArt(t *testing.T) {
	m := newArtModel(t)
	const contentW, h = 96, 12
	lines := m.nowPlayingLines(contentW, h)

	if len(lines) != h {
		t.Fatalf("got %d lines, want %d", len(lines), h)
	}
	// No composed row may exceed the content width, or the box border breaks.
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > contentW {
			t.Errorf("line %d width %d exceeds contentW %d", i, w, contentW)
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "▀") {
		t.Fatal("expected half-block art in now-playing lines")
	}
	if !strings.Contains(ansi.Strip(joined), "Test Artist") {
		t.Error("expected the artist name alongside the art")
	}
	// A middle row is within the art band, so it must start with the art column.
	if mid := ansi.Strip(lines[h/2]); !strings.HasPrefix(mid, "▀") {
		t.Errorf("expected art column on the left; mid line = %q", mid)
	}
}

func TestNowPlayingLines_NoArtWhenDisabled(t *testing.T) {
	m := newArtModel(t)
	disabled := false
	m.cfg.AlbumArt = &disabled
	if got := strings.Join(m.nowPlayingLines(96, 12), "\n"); strings.Contains(got, "▀") {
		t.Error("art should not render when AlbumArt is disabled in config")
	}
}

func TestNowPlayingLines_NoArtWithoutTrueColor(t *testing.T) {
	m := newArtModel(t)
	m.artColorOK = false
	if got := strings.Join(m.nowPlayingLines(96, 12), "\n"); strings.Contains(got, "▀") {
		t.Error("art should not render without 256-colour/truecolor support")
	}
}

func TestNowPlayingLines_NoArtWhenNarrow(t *testing.T) {
	m := newArtModel(t)
	// Too narrow to fit the cover plus a usable text column.
	if got := strings.Join(m.nowPlayingLines(30, 12), "\n"); strings.Contains(got, "▀") {
		t.Error("art should not render when the panel is too narrow")
	}
}

// TestNowPlayingLines_StaleCoverIgnored verifies the current track's ArtworkURL
// must match the loaded cover, so a cover left over from the previous track is
// not shown against a new one.
func TestNowPlayingLines_StaleCoverIgnored(t *testing.T) {
	m := newArtModel(t)
	m.playerState.Track.ArtworkURL = "https://example.test/cover/different.jpg"
	if got := strings.Join(m.nowPlayingLines(96, 12), "\n"); strings.Contains(got, "▀") {
		t.Error("art should not render when the loaded cover is for a different track")
	}
}
