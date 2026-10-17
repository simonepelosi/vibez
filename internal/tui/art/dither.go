package art

import (
	"image"
	"image/draw"
	"math"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// ditherColors is the palette size the cover is reduced to before
// dithering. 16 keeps small accents (a green cap, a red scarf) while still
// producing a visible dither texture.
const ditherColors = 16

// RenderDithered converts img into exactly size.Height strings, each of
// visual width size.Width, drawn as an Atkinson-dithered pixel image.
//
// Every cell is a sextant (U+1FB00 block) holding a 2×3 grid of solid
// sub-pixels, so the effective resolution is 2×Width by 3×Height — at the
// usual ~2:1 cell aspect those sub-pixels are close to square. Terminals
// such as Alacritty, kitty, Ghostty, WezTerm and foot draw sextants
// themselves, so they tile without the gaps braille dots leave; see
// SupportsSextants for the exception.
//
// The cover is first reduced to a small k-means palette and then
// Atkinson-dithered, which diffuses only 6/8 of the error: flat areas stay
// clean and the result reads as deliberate pixel art rather than noise. A
// cell can show only two colours, so each one is fitted to the best
// fg/bg pair among its sub-pixels.
func RenderDithered(img image.Image, size Size) []string {
	if img == nil || size.Width <= 0 || size.Height <= 0 {
		return nil
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	gw, gh := size.Width*2, size.Height*3
	px := sampleGrid(img, gw, gh)
	out := atkinson(px, gw, gh, palette(px, ditherColors))

	lines := make([]string, size.Height)
	var sub [6]rgb
	for row := range size.Height {
		var sb strings.Builder
		for col := range size.Width {
			for i := range 6 {
				sub[i] = out[(row*3+i/2)*gw+col*2+i%2]
			}
			fg, bg, mask := fitPair(sub[:])
			sb.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(hexOf(fg))).
				Background(lipgloss.Color(hexOf(bg))).
				Render(string(sextantRune(mask))))
		}
		lines[row] = sb.String()
	}
	return lines
}

// SupportsSextants reports whether the terminal can be expected to draw the
// sextant block. Apple's Terminal.app renders it from fonts that lack the
// glyphs, so callers should fall back to RenderHalfBlocks there.
func SupportsSextants() bool {
	return os.Getenv("TERM_PROGRAM") != "Apple_Terminal"
}

type rgb [3]float64

// dist2 is a "redmean" weighted RGB distance: nearly as cheap as plain
// Euclidean but much closer to perceived difference, which matters when
// picking palette entries and dither pairs.
func dist2(a, b rgb) float64 {
	rm := (a[0] + b[0]) / 2
	dr, dg, db := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return (2+rm/256)*dr*dr + 4*dg*dg + (2+(255-rm)/256)*db*db
}

// sampleGrid area-averages img into a gw × gh grid (the same box filter as
// RenderHalfBlocks) so a heavily downscaled cover stays recognisable. The
// image is converted to RGBA once up front: reading Pix directly is roughly
// an order of magnitude faster than image.At on a decoded JPEG.
func sampleGrid(img image.Image, gw, gh int) []rgb {
	b := img.Bounds()
	src, ok := img.(*image.RGBA)
	if !ok {
		src = image.NewRGBA(b)
		draw.Draw(src, b, img, b.Min, draw.Src)
	}
	srcW, srcH := b.Dx(), b.Dy()
	out := make([]rgb, gw*gh)
	for py := range gh {
		y0 := py * srcH / gh
		y1 := max((py+1)*srcH/gh, y0+1)
		for px := range gw {
			x0 := px * srcW / gw
			x1 := max((px+1)*srcW/gw, x0+1)
			var r, g, bl, n float64
			for y := y0; y < y1; y++ {
				row := src.Pix[src.PixOffset(b.Min.X+x0, b.Min.Y+y):]
				for x := range x1 - x0 {
					r += float64(row[x*4])
					g += float64(row[x*4+1])
					bl += float64(row[x*4+2])
					n++
				}
			}
			out[py*gw+px] = rgb{r / n, g / n, bl / n}
		}
	}
	return out
}

// paletteSamples caps how many pixels the k-means palette is fitted on. An
// evenly strided subsample of a few thousand pixels yields the same 16
// colours as the full grid at a fraction of the cost.
const paletteSamples = 4096

// palette picks k representative colours with k-means. Seeding is maximin
// (start at the colour nearest the mean, then repeatedly add the pixel
// farthest from every seed), which is deterministic and guarantees small
// but distinct accent colours a slot.
func palette(all []rgb, k int) []rgb {
	px := all
	if step := len(all) / paletteSamples; step > 1 {
		px = make([]rgb, 0, len(all)/step+1)
		for i := 0; i < len(all); i += step {
			px = append(px, all[i])
		}
	}
	var mean rgb
	for _, p := range px {
		for c := range 3 {
			mean[c] += p[c] / float64(len(px))
		}
	}
	cent := []rgb{px[nearest(mean, px)]}
	minD := make([]float64, len(px))
	for i, p := range px {
		minD[i] = dist2(p, cent[0])
	}
	for len(cent) < k {
		far := 0
		for i := range px {
			if minD[i] > minD[far] {
				far = i
			}
		}
		if minD[far] == 0 {
			break // fewer distinct colours than k
		}
		cent = append(cent, px[far])
		for i, p := range px {
			minD[i] = min(minD[i], dist2(p, px[far]))
		}
	}

	sums := make([]rgb, len(cent))
	cnt := make([]float64, len(cent))
	// Lloyd iterations until no centroid moves by more than about one RGB step.
	for range 10 {
		clear(sums)
		clear(cnt)
		for _, p := range px {
			j := nearest(p, cent)
			for c := range 3 {
				sums[j][c] += p[c]
			}
			cnt[j]++
		}
		moved := false
		for j := range cent {
			if cnt[j] == 0 {
				continue
			}
			next := rgb{sums[j][0] / cnt[j], sums[j][1] / cnt[j], sums[j][2] / cnt[j]}
			moved = moved || dist2(next, cent[j]) > 9
			cent[j] = next
		}
		if !moved {
			break
		}
	}
	return cent
}

func nearest(p rgb, pal []rgb) int {
	bi, bd := 0, math.MaxFloat64
	for j, c := range pal {
		if d := dist2(p, c); d < bd {
			bi, bd = j, d
		}
	}
	return bi
}

// atkinson maps every pixel to the palette, spreading 1/8 of the error to
// each of six neighbours. Dropping the remaining 2/8 is what keeps flat
// areas free of the stray speckles Floyd–Steinberg produces.
func atkinson(px []rgb, gw, gh int, pal []rgb) []rgb {
	work := append([]rgb(nil), px...)
	out := make([]rgb, len(px))
	taps := [6][2]int{{1, 0}, {2, 0}, {-1, 1}, {0, 1}, {1, 1}, {0, 2}}
	for y := range gh {
		for x := range gw {
			i := y*gw + x
			q := pal[nearest(work[i], pal)]
			out[i] = q
			for _, t := range taps {
				nx, ny := x+t[0], y+t[1]
				if nx < 0 || nx >= gw || ny >= gh {
					continue
				}
				j := ny*gw + nx
				for c := range 3 {
					work[j][c] += (work[i][c] - q[c]) / 8
				}
			}
		}
	}
	return out
}

// fitPair picks the fg/bg pair among the cell's sub-pixel colours that
// represents all of them with the least error, and returns the bitmask of
// sub-pixels drawn in fg.
func fitPair(sub []rgb) (fg, bg rgb, mask uint8) {
	cand := make([]rgb, 0, len(sub))
	for _, s := range sub {
		seen := false
		for _, c := range cand {
			seen = seen || c == s
		}
		if !seen {
			cand = append(cand, s)
		}
	}
	if len(cand) == 1 {
		return cand[0], cand[0], 0
	}
	best := math.MaxFloat64
	for i := range cand {
		for j := i + 1; j < len(cand); j++ {
			e, m := 0.0, uint8(0)
			for k, s := range sub {
				df, db := dist2(s, cand[i]), dist2(s, cand[j])
				if df < db {
					e += df
					m |= 1 << k
				} else {
					e += db
				}
			}
			if e < best {
				best, fg, bg, mask = e, cand[i], cand[j], m
			}
		}
	}
	return fg, bg, mask
}

// sextantRune returns the glyph whose filled sub-cells match mask, with bit
// i set for sub-cell i in row-major order (top-left, top-right, …). The
// U+1FB00 block omits the four patterns that already exist elsewhere.
func sextantRune(mask uint8) rune {
	switch mask {
	case 0:
		return ' '
	case 0b010101:
		return '▌'
	case 0b101010:
		return '▐'
	case 0b111111:
		return '█'
	}
	r := 0x1FB00 + rune(mask) - 1
	if mask > 0b010101 {
		r--
	}
	if mask > 0b101010 {
		r--
	}
	return r
}

func hexOf(c rgb) string {
	u := func(v float64) uint8 { return uint8(min(255, max(0, math.Round(v)))) }
	return hexColor(u(c[0]), u(c[1]), u(c[2]))
}
