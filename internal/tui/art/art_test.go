package art

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderHalfBlocks_DimensionsAndContent(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			//nolint:gosec // x,y ∈ [0,16) so x*16 ∈ [0,240] — fits in uint8.
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 255})
		}
	}

	const cols, rows = 12, 6
	lines := RenderHalfBlocks(img, Size{Width: cols, Height: rows})
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

// TestRenderHalfBlocks_TopBottomColors checks that the top pixel drives the
// foreground and the bottom pixel drives the background: a cover split red
// (top) over blue (bottom) should emit both colours in the single rendered row.
func TestRenderHalfBlocks_TopBottomColors(t *testing.T) {
	// 2px tall: row 0 red, row 1 blue → one output row.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for x := range 2 {
		img.SetNRGBA(x, 0, color.NRGBA{R: 255, A: 255})
		img.SetNRGBA(x, 1, color.NRGBA{B: 255, A: 255})
	}

	lines := RenderHalfBlocks(img, Size{Width: 2, Height: 1})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	// Foreground (top) red = 38;2;255;0;0, background (bottom) blue = 48;2;0;0;255.
	if !strings.Contains(lines[0], "38;2;255;0;0") {
		t.Errorf("expected red foreground in %q", lines[0])
	}
	if !strings.Contains(lines[0], "48;2;0;0;255") {
		t.Errorf("expected blue background in %q", lines[0])
	}
}

// TestRenderHalfBlocks_AreaAveragesPixels checks that downscaling averages all
// source pixels mapped to a cell rather than picking a single nearest pixel:
// a half-red/half-black region must come out mid-red, not pure red or black.
func TestRenderHalfBlocks_AreaAveragesPixels(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 4))
	// Top 2×2 region: red/black checkerboard → average R = 510/4 = 127.
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, A: 255})
	// Bottom 2×2 region: solid blue.
	for x := range 2 {
		img.SetNRGBA(x, 2, color.NRGBA{B: 255, A: 255})
		img.SetNRGBA(x, 3, color.NRGBA{B: 255, A: 255})
	}

	lines := RenderHalfBlocks(img, Size{Width: 1, Height: 1})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "38;2;127;0;0") {
		t.Errorf("expected averaged foreground 38;2;127;0;0 in %q", lines[0])
	}
	if strings.Contains(lines[0], "38;2;255;0;0") {
		t.Errorf("foreground is a single source pixel, not an average: %q", lines[0])
	}
	if !strings.Contains(lines[0], "48;2;0;0;255") {
		t.Errorf("expected solid blue background in %q", lines[0])
	}
}

func TestRenderHalfBlocks_RejectsNonPositiveSize(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	cases := []Size{{Width: 0, Height: 1}, {Width: 1, Height: 0}, {Width: -1, Height: 1}, {Width: 1, Height: -1}}
	for _, tc := range cases {
		if got := RenderHalfBlocks(img, tc); len(got) != 0 {
			t.Fatalf("RenderHalfBlocks(%+v) returned %d lines, want 0", tc, len(got))
		}
	}
	if got := RenderHalfBlocks(nil, Size{Width: 1, Height: 1}); len(got) != 0 {
		t.Fatalf("RenderHalfBlocks(nil) returned %d lines, want 0", len(got))
	}
}

func TestDecode_SupportsPNGAndJPEG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 90, G: 80, B: 70, A: 255})

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	if decoded, err := Decode(&pngBuf); err != nil || decoded.Bounds().Dx() != 1 {
		t.Fatalf("Decode(PNG) = %v, %v; want image, nil", decoded, err)
	}

	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if decoded, err := Decode(&jpegBuf); err != nil || decoded.Bounds().Dy() != 1 {
		t.Fatalf("Decode(JPEG) = %v, %v; want image, nil", decoded, err)
	}
}

func TestDecode_RejectsUnsupportedBytes(t *testing.T) {
	if _, err := Decode(strings.NewReader("not an image")); err == nil {
		t.Fatal("Decode(invalid) error = nil, want error")
	}
}

func TestFetchAndDecode_SupportsPNGDataURL(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 12, G: 34, B: 56, A: 255})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	decoded, err := FetchAndDecode(t.Context(), nil, url, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 1 || decoded.Bounds().Dy() != 1 {
		t.Fatalf("decoded bounds = %v, want 1x1", decoded.Bounds())
	}
}

func TestDecodeBounded_RejectsOverByteCap(t *testing.T) {
	if _, err := decodeBounded(strings.NewReader("abcdef"), 5); err == nil || !strings.Contains(err.Error(), "exceeds max bytes") {
		t.Fatalf("decodeBounded(over cap) error = %v, want exceeds max bytes", err)
	}
}

func TestDecodeBounded_RejectsOversizedDimensions(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, MaxArtworkWidth+1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBounded(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err == nil || !strings.Contains(err.Error(), "exceed max") {
		t.Fatalf("decodeBounded(oversized) error = %v, want exceed max", err)
	}
}
