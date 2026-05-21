package art

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
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
	if client == nil {
		return nil, fmt.Errorf("fetch artwork: nil http client")
	}
	if url == "" {
		return nil, fmt.Errorf("fetch artwork: empty url")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("fetch artwork: non-positive max bytes %d", maxBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	return Decode(io.LimitReader(resp.Body, maxBytes))
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
			sb.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.r, top.g, top.b, bottom.r, bottom.g, bottom.b))
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
