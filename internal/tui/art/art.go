// Package art renders album artwork as coloured Unicode half-block art for the
// now-playing panel.
//
// Each character cell is drawn as an upper-half block "▀" whose foreground
// colour is the top pixel and whose background colour is the bottom pixel, so a
// single cell encodes two vertically-stacked image pixels. Because a typical
// terminal cell is about twice as tall as it is wide, this yields roughly
// square pixels — a square cover looks square when rendered with cols == rows*2.
package art

import (
	"bytes"
	"fmt"
	"image"

	// Register the decoders for the formats Apple Music serves artwork in.
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"charm.land/lipgloss/v2"
)

// upperHalfBlock fills the top half of a cell; the bottom half shows through as
// the cell's background colour.
const upperHalfBlock = "▀"

// Decode decodes JPEG or PNG image bytes into an image.Image.
func Decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// Render converts img into exactly rows strings, each of visual width cols,
// drawn with 24-bit colour half-blocks. The image is nearest-neighbour scaled
// to fill a cols × (rows*2) pixel grid, so callers should size cols/rows to the
// image's aspect ratio (for a square cover, cols == rows*2).
//
// Colours are emitted through lipgloss, so the active renderer down-samples them
// to the terminal's colour profile automatically. Render returns nil for a nil
// image or non-positive dimensions.
func Render(img image.Image, cols, rows int) []string {
	if img == nil || cols <= 0 || rows <= 0 {
		return nil
	}

	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return nil
	}

	pxW, pxH := cols, rows*2

	// sample returns the average 8-bit RGB of every source pixel that maps to
	// grid cell (px, py) in the pxW × pxH target grid. Area-averaging (a box
	// filter) is what keeps a heavily downscaled cover smooth and recognisable;
	// picking a single nearest pixel per cell instead produces a harsh, aliased
	// result at this size.
	sample := func(px, py int) (uint8, uint8, uint8) {
		x0 := b.Min.X + px*srcW/pxW
		x1 := b.Min.X + (px+1)*srcW/pxW
		y0 := b.Min.Y + py*srcH/pxH
		y1 := b.Min.Y + (py+1)*srcH/pxH
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if y1 <= y0 {
			y1 = y0 + 1
		}
		var rs, gs, bs, n uint64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				r, g, bl, _ := img.At(x, y).RGBA()
				rs += uint64(r >> 8)
				gs += uint64(g >> 8)
				bs += uint64(bl >> 8)
				n++
			}
		}
		if n == 0 {
			n = 1
		}
		return uint8(rs / n), uint8(gs / n), uint8(bs / n) //nolint:gosec // averaged 8-bit channels
	}

	lines := make([]string, rows)
	for row := range rows {
		var sb strings.Builder
		for col := range cols {
			tr, tg, tb := sample(col, row*2)   // top pixel → foreground
			br, bg, bb := sample(col, row*2+1) // bottom pixel → background
			cell := lipgloss.NewStyle().
				Foreground(lipgloss.Color(hexColor(tr, tg, tb))).
				Background(lipgloss.Color(hexColor(br, bg, bb))).
				Render(upperHalfBlock)
			sb.WriteString(cell)
		}
		lines[row] = sb.String()
	}
	return lines
}

func hexColor(r, g, b uint8) string {
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
