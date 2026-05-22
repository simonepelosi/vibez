package art

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderHalfBlocks_RendersExactHalfBlockANSI(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{R: 4, G: 5, B: 6, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 7, G: 8, B: 9, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 10, G: 11, B: 12, A: 255})

	lines := RenderHalfBlocks(img, Size{Width: 2, Height: 1})
	want := []string{
		"\x1b[38;2;1;2;3m\x1b[48;2;4;5;6m▀" +
			"\x1b[38;2;7;8;9m\x1b[48;2;10;11;12m▀" +
			"\x1b[0m",
	}
	if len(lines) != len(want) {
		t.Fatalf("line count = %d, want %d: %#v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
		if got := lipgloss.Width(lines[i]); got != 2 {
			t.Fatalf("line %d visual width = %d, want 2", i, got)
		}
	}
}

func TestRenderHalfBlocks_ScalesImageToRequestedOutput(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	colors := []color.NRGBA{
		{R: 1, G: 0, B: 0, A: 255}, {R: 2, G: 0, B: 0, A: 255}, {R: 3, G: 0, B: 0, A: 255}, {R: 4, G: 0, B: 0, A: 255},
		{R: 5, G: 0, B: 0, A: 255}, {R: 6, G: 0, B: 0, A: 255}, {R: 7, G: 0, B: 0, A: 255}, {R: 8, G: 0, B: 0, A: 255},
		{R: 9, G: 0, B: 0, A: 255}, {R: 10, G: 0, B: 0, A: 255}, {R: 11, G: 0, B: 0, A: 255}, {R: 12, G: 0, B: 0, A: 255},
		{R: 13, G: 0, B: 0, A: 255}, {R: 14, G: 0, B: 0, A: 255}, {R: 15, G: 0, B: 0, A: 255}, {R: 16, G: 0, B: 0, A: 255},
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, colors[y*4+x])
		}
	}

	lines := RenderHalfBlocks(img, Size{Width: 2, Height: 2})
	want := []string{
		"\x1b[38;2;1;0;0m\x1b[48;2;5;0;0m▀" +
			"\x1b[38;2;3;0;0m\x1b[48;2;7;0;0m▀" +
			"\x1b[0m",
		"\x1b[38;2;9;0;0m\x1b[48;2;13;0;0m▀" +
			"\x1b[38;2;11;0;0m\x1b[48;2;15;0;0m▀" +
			"\x1b[0m",
	}
	if len(lines) != len(want) {
		t.Fatalf("line count = %d, want %d: %#v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRenderHalfBlocks_GorillazArtworkMatchesGolden(t *testing.T) {
	f, err := os.Open("testdata/gorillaz_2001_album.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	img, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	lines := RenderHalfBlocks(img, Size{Width: 16, Height: 8})
	got := strings.Join(lines, "\n") + "\n"

	wantBytes, err := os.ReadFile("testdata/gorillaz_2001_album_16x8.ansi")
	if err != nil {
		t.Fatal(err)
	}
	want := string(wantBytes)
	if got != want {
		t.Fatalf("rendered Gorillaz artwork mismatch\ngot:\n%q\nwant:\n%q", got, want)
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

func TestSupportsTrueColor_ReadsCOLORTERM(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "truecolor", value: "truecolor", want: true},
		{name: "24bit", value: "24bit", want: true},
		{name: "mixed case", value: "TRUECOLOR", want: true},
		{name: "empty", value: "", want: false},
		{name: "256color", value: "256color", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COLORTERM", tc.value)
			if got := SupportsTrueColor(); got != tc.want {
				t.Fatalf("SupportsTrueColor() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFetchAndDecode_SupportsLocalArtworkPath(t *testing.T) {
	img, err := FetchAndDecode(t.Context(), nil, "testdata/gorillaz_2001_album.png", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Fatalf("decoded bounds = %v, want non-empty", img.Bounds())
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
