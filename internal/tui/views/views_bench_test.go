package views

import (
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
)

func BenchmarkRenderGlowTitle(b *testing.B) {
	const title = "Around the World (Remastered)"
	b.ReportAllocs()
	step := 0
	for b.Loop() {
		_ = RenderGlowTitle(title, step)
		step++
	}
}

func BenchmarkRenderProgressBar(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = RenderProgressBar(95*time.Second, 240*time.Second, 48)
	}
}

func BenchmarkEQView(b *testing.B) {
	eq := NewEqualizer(player.DefaultEQBands())
	eq.SetSize(80, 24)
	b.ReportAllocs()
	for b.Loop() {
		_ = eq.View()
	}
}
