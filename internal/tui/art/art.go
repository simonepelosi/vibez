package art

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"strings"
)

const (
	MaxArtworkWidth  = 4096
	MaxArtworkHeight = 4096
	MaxArtworkPixels = 16_000_000
)

type Size struct {
	Width  int
	Height int
}

func SupportsTrueColor() bool {
	value := strings.ToLower(os.Getenv("COLORTERM"))
	return value == "truecolor" || value == "24bit"
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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch artwork: status %d", resp.StatusCode)
	}
	return decodeBounded(resp.Body, maxBytes)
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

func RenderHalfBlocks(img image.Image, size Size) []string {
	if img == nil || size.Width <= 0 || size.Height <= 0 {
		return nil
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil
	}

	lines := make([]string, size.Height)
	for row := 0; row < size.Height; row++ {
		var sb strings.Builder
		for col := 0; col < size.Width; col++ {
			top := sample(img, bounds, col, row*2, size.Width, size.Height*2)
			bottom := sample(img, bounds, col, row*2+1, size.Width, size.Height*2)
			_, _ = fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.r, top.g, top.b, bottom.r, bottom.g, bottom.b)
		}
		sb.WriteString("\x1b[0m")
		lines[row] = sb.String()
	}
	return lines
}

type rgb struct{ r, g, b uint8 }

func sample(img image.Image, bounds image.Rectangle, outX, outY, outW, outH int) rgb {
	if outW <= 0 || outH <= 0 {
		panic("invalid output size")
	}
	x := bounds.Min.X + outX*bounds.Dx()/outW
	y := bounds.Min.Y + outY*bounds.Dy()/outH
	if x >= bounds.Max.X {
		x = bounds.Max.X - 1
	}
	if y >= bounds.Max.Y {
		y = bounds.Max.Y - 1
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return rgb{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}
