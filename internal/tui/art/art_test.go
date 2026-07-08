package art

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// makePNG builds a src.png-encoded image of size w×h filled by fn(x, y).
func makePNG(t *testing.T, w, h int, fn func(x, y int) color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, fn(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestDecodeRoundTrip(t *testing.T) {
	data := makePNG(t, 4, 4, func(_, _ int) color.Color { return color.White })
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := img.Bounds().Dx(); got != 4 {
		t.Fatalf("width = %d, want 4", got)
	}
}

func TestDecodeInvalid(t *testing.T) {
	if _, err := Decode([]byte("not an image")); err == nil {
		t.Fatal("expected error decoding garbage, got nil")
	}
}

func TestRenderDimensions(t *testing.T) {
	data := makePNG(t, 16, 16, func(x, y int) color.Color {
		//nolint:gosec // x,y ∈ [0,16) so x*16 ∈ [0,240] — fits in uint8.
		return color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255}
	})
	img, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	const cols, rows = 12, 6
	lines := Render(img, cols, rows)
	if len(lines) != rows {
		t.Fatalf("got %d lines, want %d", len(lines), rows)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != cols {
			t.Errorf("line %d visual width = %d, want %d", i, w, cols)
		}
		// Stripped of colour, each line is exactly `cols` half-block runes.
		if stripped := ansi.Strip(ln); stripped != strings.Repeat(upperHalfBlock, cols) {
			t.Errorf("line %d stripped = %q, want %d half-blocks", i, stripped, cols)
		}
	}
}

func TestRenderGuards(t *testing.T) {
	data := makePNG(t, 4, 4, func(_, _ int) color.Color { return color.Black })
	img, _ := Decode(data)
	if lines := Render(nil, 4, 4); lines != nil {
		t.Error("Render(nil image) should return nil")
	}
	if lines := Render(img, 0, 4); lines != nil {
		t.Error("Render(cols=0) should return nil")
	}
	if lines := Render(img, 4, 0); lines != nil {
		t.Error("Render(rows=0) should return nil")
	}
}

// TestRenderColors checks that the top pixel drives the foreground and the
// bottom pixel drives the background: a cover split red (top) over blue (bottom)
// should emit both colours in the single rendered row.
func TestRenderColors(t *testing.T) {
	// 2px tall: row 0 red, row 1 blue → one output row.
	data := makePNG(t, 2, 2, func(_, y int) color.Color {
		if y == 0 {
			return color.RGBA{R: 255, A: 255}
		}
		return color.RGBA{B: 255, A: 255}
	})
	img, _ := Decode(data)
	lines := Render(img, 2, 1)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	// Foreground (top) red = 38;2;255;0;0, background (bottom) blue = 48;2;0;0;255.
	if !strings.Contains(lines[0], "255;0;0") {
		t.Errorf("expected red foreground in %q", lines[0])
	}
	if !strings.Contains(lines[0], "0;0;255") {
		t.Errorf("expected blue background in %q", lines[0])
	}
}
