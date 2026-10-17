package art

import (
	"image"
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSextantRuneCoversAllMasks(t *testing.T) {
	seen := map[rune]uint8{}
	for m := range uint8(64) {
		r := sextantRune(m)
		if prev, dup := seen[r]; dup {
			t.Fatalf("masks %d and %d both map to %q", prev, m, r)
		}
		seen[r] = m
	}
	// Spot-check against the Unicode names: 🬀 is SEXTANT-1, 🬻 is SEXTANT-23456.
	if got := sextantRune(0b000001); got != 0x1FB00 {
		t.Errorf("sextant-1 = %U, want U+1FB00", got)
	}
	if got := sextantRune(0b111110); got != 0x1FB3B {
		t.Errorf("sextant-23456 = %U, want U+1FB3B", got)
	}
}

func TestRenderDitheredSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), 128, 255})
		}
	}
	lines := RenderDithered(img, Size{Width: 12, Height: 5})
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 12 {
			t.Errorf("line %d width = %d, want 12", i, w)
		}
	}
	if RenderDithered(nil, Size{Width: 4, Height: 4}) != nil {
		t.Error("nil image should render nothing")
	}
}
