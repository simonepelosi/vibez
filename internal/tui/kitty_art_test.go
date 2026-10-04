package tui

import (
	"image"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider"
)

func TestGhosttyArtworkUsesImagePlaceholders(t *testing.T) {
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("TMUX", "")
	m := newModel(nil)
	m.artMode = true
	m.supportsArtGraphics = func() bool { return true }
	m.supportsArtColor = func() bool { return true }
	m.artwork.url = "https://example.invalid/cover.png"
	m.artwork.img = image.NewNRGBA(image.Rect(0, 0, 512, 512))
	m.playerState.Track = &provider.Track{ArtworkURL: m.artwork.url}
	lines := strings.Join(m.nowPlayingArtLines(80, 20), "\n")
	if !strings.ContainsRune(lines, '\U0010EEEE') || strings.ContainsRune(lines, '▀') {
		t.Fatal("Ghostty cover still uses pixelated character blocks")
	}
}

func TestArtworkGraphicsLifecycle(t *testing.T) {
	m := newModel(nil)
	m.supportsArtGraphics = func() bool { return true }
	m.supportsArtColor = func() bool { return true }
	m.artMode = true
	m.width, m.height = 100, 40
	m.artwork.img = image.NewNRGBA(image.Rect(0, 0, 512, 512))
	m.artwork.url = "cover"
	m.playerState.Track = &provider.Track{ArtworkURL: "cover"}
	if m.syncArtworkGraphics() == nil {
		t.Fatal("first cover must upload")
	}
	if m.syncArtworkGraphics() != nil {
		t.Fatal("unchanged cover uploaded again")
	}
	m.width = 20
	if m.syncArtworkGraphics() == nil {
		t.Fatal("resize must update placement")
	}
	m.artworkGen++
	if m.syncArtworkGraphics() == nil {
		t.Fatal("new cover must replace old image")
	}
	m.artMode = false
	if m.syncArtworkGraphics() == nil || m.artGraphics.id != 0 {
		t.Fatal("hidden cover resources not released")
	}
}
