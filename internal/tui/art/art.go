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
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

const (
	MaxArtworkWidth      = 4096
	MaxArtworkHeight     = 4096
	MaxArtworkPixels     = 16_000_000
	MaxDecodedArtworkDim = 256
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
	img, err := Decode(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	return Downsample(img, MaxDecodedArtworkDim), nil
}

// Downsample scales img down with box averaging so neither dimension exceeds
// maxDim, returning an *image.RGBA to avoid retaining multi-megabyte backing
// pixel buffers in memory. Images already within maxDim on both axes are
// returned unchanged.
func Downsample(img image.Image, maxDim int) image.Image {
	if img == nil || maxDim <= 0 {
		return img
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || (w <= maxDim && h <= maxDim) {
		return img
	}

	var dstW, dstH int
	if w >= h {
		dstW = maxDim
		dstH = max(1, (h*maxDim+w/2)/w)
	} else {
		dstH = maxDim
		dstW = max(1, (w*maxDim+h/2)/h)
	}

	src := newPixelSource(img)
	var xRangeBuf [256][2]int
	xRanges := computeXRanges(xRangeBuf[:], dstW, src.srcW)
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for dy := range dstH {
		y0 := (dy * src.srcH) / dstH
		y1 := max(((dy+1)*src.srcH)/dstH, y0+1)
		row := dst.Pix[dy*dst.Stride:]
		for dx := range dstW {
			xr := xRanges[dx]
			r, g, b := src.averageBox(xr[0], y0, xr[1], y1)
			off := dx * 4
			row[off] = r
			row[off+1] = g
			row[off+2] = b
			row[off+3] = 0xff
		}
	}
	return dst
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
	src := newPixelSource(img)
	if src.srcW <= 0 || src.srcH <= 0 {
		return nil
	}

	pxW, pxH := size.Width, size.Height*2
	var xRangeBuf [256][2]int
	xRanges := computeXRanges(xRangeBuf[:], pxW, src.srcW)

	lines := make([]string, size.Height)
	buf := make([]byte, 0, size.Width*44)
	for row := range size.Height {
		ty0 := (row * 2) * src.srcH / pxH
		ty1 := max((row*2+1)*src.srcH/pxH, ty0+1)
		by0 := (row*2 + 1) * src.srcH / pxH
		by1 := max((row*2+2)*src.srcH/pxH, by0+1)

		buf = buf[:0]
		styled := false
		var pTR, pTG, pTB, pBR, pBG, pBB uint8
		for col := range size.Width {
			xr := xRanges[col]
			tr, tg, tb := src.averageBox(xr[0], ty0, xr[1], ty1)
			br, bg, bb := src.averageBox(xr[0], by0, xr[1], by1)
			if !styled || tr != pTR || tg != pTG || tb != pTB || br != pBR || bg != pBG || bb != pBB {
				buf = appendCellSGR(buf, tr, tg, tb, br, bg, bb)
				pTR, pTG, pTB, pBR, pBG, pBB = tr, tg, tb, br, bg, bb
				styled = true
			}
			buf = append(buf, upperHalfBlock...)
		}
		if styled {
			buf = append(buf, "\x1b[m"...)
		}
		lines[row] = string(buf)
	}
	return lines
}

func computeXRanges(scratch [][2]int, dstW, srcW int) [][2]int {
	var out [][2]int
	if dstW <= len(scratch) {
		out = scratch[:dstW]
	} else {
		out = make([][2]int, dstW)
	}
	for px := range dstW {
		x0 := px * srcW / dstW
		x1 := max((px+1)*srcW/dstW, x0+1)
		out[px] = [2]int{x0, x1}
	}
	return out
}

type pixelSource struct {
	pix     []uint8
	baseOff int
	stride  int
	srcW    int
	srcH    int
	nrgba   bool
}

func newPixelSource(img image.Image) pixelSource {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return pixelSource{srcW: srcW, srcH: srcH}
	}
	switch v := img.(type) {
	case *image.RGBA:
		return pixelSource{
			pix:     v.Pix,
			baseOff: v.PixOffset(b.Min.X, b.Min.Y),
			stride:  v.Stride,
			srcW:    srcW,
			srcH:    srcH,
		}
	case *image.NRGBA:
		return pixelSource{
			pix:     v.Pix,
			baseOff: v.PixOffset(b.Min.X, b.Min.Y),
			stride:  v.Stride,
			srcW:    srcW,
			srcH:    srcH,
			nrgba:   true,
		}
	default:
		dst := image.NewRGBA(b)
		draw.Draw(dst, b, img, b.Min, draw.Src)
		return pixelSource{
			pix:     dst.Pix,
			baseOff: dst.PixOffset(b.Min.X, b.Min.Y),
			stride:  dst.Stride,
			srcW:    srcW,
			srcH:    srcH,
		}
	}
}

func (s pixelSource) sumBox(x0, y0, x1, y1 int) (rs, gs, bs, n uint64) {
	dx := x1 - x0
	if !s.nrgba {
		for y := y0; y < y1; y++ {
			row := s.pix[s.baseOff+y*s.stride+x0*4:]
			for x := range dx {
				off := x * 4
				rs += uint64(row[off])
				gs += uint64(row[off+1])
				bs += uint64(row[off+2])
			}
			n += uint64(dx) //nolint:gosec // dx is positive pixel span within image bounds
		}
		return rs, gs, bs, n
	}

	for y := y0; y < y1; y++ {
		row := s.pix[s.baseOff+y*s.stride+x0*4:]
		for x := range dx {
			off := x * 4
			a := uint32(row[off+3])
			if a == 0xff {
				rs += uint64(row[off])
				gs += uint64(row[off+1])
				bs += uint64(row[off+2])
			} else if a != 0 {
				// Match color.NRGBA.RGBA()>>8 premultiplication for translucent pixels.
				r16 := (uint32(row[off]) * 0x101 * a) / 0xff
				g16 := (uint32(row[off+1]) * 0x101 * a) / 0xff
				b16 := (uint32(row[off+2]) * 0x101 * a) / 0xff
				rs += uint64(r16 >> 8)
				gs += uint64(g16 >> 8)
				bs += uint64(b16 >> 8)
			}
		}
		n += uint64(dx) //nolint:gosec // dx is positive pixel span within image bounds
	}
	return rs, gs, bs, n
}

func (s pixelSource) averageBox(x0, y0, x1, y1 int) (uint8, uint8, uint8) {
	rs, gs, bs, n := s.sumBox(x0, y0, x1, y1)
	if n == 0 {
		return 0, 0, 0
	}
	return uint8(rs / n), uint8(gs / n), uint8(bs / n) //nolint:gosec // averaged 8-bit channels
}

func appendCellSGR(buf []byte, fgR, fgG, fgB, bgR, bgG, bgB uint8) []byte {
	buf = append(buf, "\x1b[38;2;"...)
	buf = strconv.AppendUint(buf, uint64(fgR), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(fgG), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(fgB), 10)
	buf = append(buf, ";48;2;"...)
	buf = strconv.AppendUint(buf, uint64(bgR), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(bgG), 10)
	buf = append(buf, ';')
	buf = strconv.AppendUint(buf, uint64(bgB), 10)
	return append(buf, 'm')
}
