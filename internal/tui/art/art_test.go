package art

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderHalfBlocks_Uses24BitANSIForegroundAndBackground(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{R: 4, G: 5, B: 6, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 7, G: 8, B: 9, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 10, G: 11, B: 12, A: 255})

	lines := RenderHalfBlocks(img, Size{Width: 2, Height: 1})
	if len(lines) != 1 {
		t.Fatalf("line count = %d, want 1", len(lines))
	}
	line := lines[0]
	for _, want := range []string{"\x1b[38;2;1;2;3m", "\x1b[48;2;4;5;6m", "\x1b[38;2;7;8;9m", "\x1b[48;2;10;11;12m", "▀", "\x1b[0m"} {
		if !strings.Contains(line, want) {
			t.Fatalf("rendered line missing %q: %q", want, line)
		}
	}
	if got := lipgloss.Width(line); got != 2 {
		t.Fatalf("visual width = %d, want 2", got)
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
