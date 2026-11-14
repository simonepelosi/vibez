package tui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/art"
)

func makeModelBenchImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := range 128 {
		for x := range 128 {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 2), //nolint:gosec // G115: x in [0,127]
				G: uint8(y * 2), //nolint:gosec // G115: y in [0,127]
				B: 180,
				A: 255,
			})
		}
	}
	return img
}

func BenchmarkModelView(b *testing.B) {
	m := New(testCfg(), &mockProvider{}, newMockPlayer(), Options{})
	m.introStep = introDone
	m.width = 120
	m.height = 40
	m.artMode = true
	m.renderArt = art.RenderHalfBlocks
	m.playerState.Playing = true
	m.playerState.Position = 85 * time.Second
	m.playerState.Track = &provider.Track{
		ID:       "bench-1",
		Title:    "Digital Love",
		Artist:   "Daft Punk",
		Album:    "Discovery",
		Duration: 301 * time.Second,
	}
	m.artwork = artworkCache{
		url:      "bench://cover",
		img:      makeModelBenchImage(),
		rendered: make(map[art.Size][]string),
	}
	m.queueTracks = []provider.Track{
		*m.playerState.Track,
		{ID: "bench-2", Title: "Harder, Better, Faster, Stronger", Artist: "Daft Punk", Duration: 224 * time.Second},
		{ID: "bench-3", Title: "Crescendolls", Artist: "Daft Punk", Duration: 211 * time.Second},
	}

	b.ReportAllocs()
	for b.Loop() {
		v := m.View()
		if v.Content == "" {
			b.Fatal("empty view")
		}
	}
}
