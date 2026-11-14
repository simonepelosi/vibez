package art

import (
	"image"
	"image/color"
	"testing"
)

func makeBenchNRGBA(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			r := uint8((x * 255) / max(1, w-1))          //nolint:gosec // G115: bounded to [0,255]
			g := uint8((y * 255) / max(1, h-1))          //nolint:gosec // G115: bounded to [0,255]
			bl := uint8(((x + y) * 255) / max(1, w+h-2)) //nolint:gosec // G115: bounded to [0,255]
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: bl, A: 255})
		}
	}
	return img
}

func makeBenchRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			r := uint8((x * 255) / max(1, w-1))          //nolint:gosec // G115: bounded to [0,255]
			g := uint8((y * 255) / max(1, h-1))          //nolint:gosec // G115: bounded to [0,255]
			bl := uint8(((x + y) * 255) / max(1, w+h-2)) //nolint:gosec // G115: bounded to [0,255]
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: bl, A: 255})
		}
	}
	return img
}

func BenchmarkRenderHalfBlocks_NRGBA(b *testing.B) {
	img := makeBenchNRGBA(300, 300)
	size := Size{Width: 32, Height: 16}

	b.ReportAllocs()
	for b.Loop() {
		lines := RenderHalfBlocks(img, size)
		if len(lines) != size.Height {
			b.Fatalf("lines = %d, want %d", len(lines), size.Height)
		}
	}
}

func BenchmarkRenderHalfBlocks_RGBA(b *testing.B) {
	img := makeBenchRGBA(300, 300)
	size := Size{Width: 32, Height: 16}

	b.ReportAllocs()
	for b.Loop() {
		lines := RenderHalfBlocks(img, size)
		if len(lines) != size.Height {
			b.Fatalf("lines = %d, want %d", len(lines), size.Height)
		}
	}
}

func BenchmarkRenderDithered_NRGBA(b *testing.B) {
	img := makeBenchNRGBA(300, 300)
	size := Size{Width: 32, Height: 16}

	b.ReportAllocs()
	for b.Loop() {
		lines := RenderDithered(img, size)
		if len(lines) != size.Height {
			b.Fatalf("lines = %d, want %d", len(lines), size.Height)
		}
	}
}
