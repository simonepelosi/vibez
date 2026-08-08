// Package art fetches and renders album artwork as coloured Unicode
// half-block art for the now-playing panel.
//
// Each character cell is drawn as an upper-half block "▀" whose foreground
// colour is the top pixel and whose background colour is the bottom pixel, so
// a single cell encodes two vertically-stacked image pixels. Because a typical
// terminal cell is about twice as tall as it is wide, this yields roughly
// square pixels — a square cover looks square when rendered with
// cols ≈ rows*2.
package art

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

const (
	MaxArtworkWidth  = 4096
	MaxArtworkHeight = 4096
	MaxArtworkPixels = 16_000_000
)

// upperHalfBlock fills the top half of a cell; the bottom half shows through
// as the cell's background colour.
const upperHalfBlock = "▀"

type Size struct {
	Width  int
	Height int
}

// SupportsColor reports whether the terminal has enough colours for album art
// to look reasonable (at least 256). Colours are emitted through lipgloss, so
// truecolor output is down-sampled to the terminal's profile automatically;
// below 256 colours the result is unrecognisable and we skip the artwork (and
// its download) entirely.
func SupportsColor() bool {
	return colorprofile.Detect(os.Stdout, os.Environ()) >= colorprofile.ANSI256
}

func Decode(r io.Reader) (image.Image, error) {
	if r == nil {
		return nil, fmt.Errorf("decode artwork: nil reader")
	}
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func FetchAndDecode(ctx context.Context, client *http.Client, url string, maxBytes int64) (image.Image, error) {
	if ctx == nil {
		return nil, fmt.Errorf("fetch artwork: nil context")
	}
	if url == "" {
		return nil, fmt.Errorf("fetch artwork: empty url")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("fetch artwork: non-positive max bytes %d", maxBytes)
	}

	parsed, err := urlpkg.Parse(url)
	if err != nil {
		return nil, err
	}
	switch parsed.Scheme {
	case "http", "https":
	case "data":
		return decodeDataURL(url, maxBytes)
	case "":
		return nil, fmt.Errorf("fetch artwork: missing URL scheme")
	default:
		return nil, fmt.Errorf("fetch artwork: unsupported URL scheme %q", parsed.Scheme)
	}

	if client == nil {
		return nil, fmt.Errorf("fetch artwork: nil http client")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // G107: URL scheme is explicitly limited to http/https above before fetching artwork
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Response body close errors are not actionable after the body is read.
			return
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch artwork: status %d", resp.StatusCode)
	}
	return decodeBounded(resp.Body, maxBytes)
}

func decodeDataURL(raw string, maxBytes int64) (image.Image, error) {
	meta, payload, ok := strings.Cut(raw, ",")
	if !ok {
		return nil, fmt.Errorf("decode artwork: invalid data URL")
	}
	mediaType := strings.ToLower(strings.TrimPrefix(meta, "data:"))
	if !strings.HasPrefix(mediaType, "image/png;") && !strings.HasPrefix(mediaType, "image/jpeg;") {
		return nil, fmt.Errorf("decode artwork: unsupported data media type %q", mediaType)
	}
	if !strings.Contains(mediaType, ";base64") {
		return nil, fmt.Errorf("decode artwork: unsupported data encoding")
	}
	return decodeBounded(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)), maxBytes)
}

func decodeBounded(r io.Reader, maxBytes int64) (image.Image, error) {
	if r == nil {
		return nil, fmt.Errorf("decode artwork: nil reader")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("decode artwork: non-positive max bytes %d", maxBytes)
	}
	buf, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxBytes {
		return nil, fmt.Errorf("decode artwork: exceeds max bytes %d", maxBytes)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("decode artwork: invalid dimensions %dx%d", cfg.Width, cfg.Height)
	}
	if cfg.Width > MaxArtworkWidth || cfg.Height > MaxArtworkHeight {
		return nil, fmt.Errorf("decode artwork: dimensions %dx%d exceed max %dx%d", cfg.Width, cfg.Height, MaxArtworkWidth, MaxArtworkHeight)
	}
	if cfg.Width > MaxArtworkPixels/cfg.Height {
		return nil, fmt.Errorf("decode artwork: pixels %dx%d exceed max %d", cfg.Width, cfg.Height, MaxArtworkPixels)
	}
	return Decode(bytes.NewReader(buf))
}

// RenderHalfBlocks converts img into exactly size.Height strings, each of
// visual width size.Width, drawn with coloured half-blocks. The image is
// scaled to fill a Width × (Height*2) pixel grid, so callers pick the size to
// match the image's aspect ratio and the terminal's cell proportions.
//
// Colours are emitted through lipgloss, so the active renderer down-samples
// them to the terminal's colour profile automatically. RenderHalfBlocks
// returns nil for a nil image or non-positive dimensions.
func RenderHalfBlocks(img image.Image, size Size) []string {
	if img == nil || size.Width <= 0 || size.Height <= 0 {
		return nil
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil
	}

	pxW, pxH := size.Width, size.Height*2

	// sample returns the average 8-bit RGB of every source pixel that maps to
	// grid cell (px, py) in the pxW × pxH target grid. Area-averaging (a box
	// filter) is what keeps a heavily downscaled cover smooth and
	// recognisable; picking a single nearest pixel per cell instead produces
	// a harsh, aliased result at this size.
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

	lines := make([]string, size.Height)
	for row := range size.Height {
		var sb strings.Builder
		for col := range size.Width {
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
