package art

import (
	"image"
	"math"
	"os"
	"unicode/utf8"
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
	buf := make([]byte, 0, size.Width*44)
	var sub [6]rgb
	for row := range size.Height {
		buf = buf[:0]
		styled := false
		var pFR, pFG, pFB, pBR, pBG, pBB uint8
		for col := range size.Width {
			for i := range 6 {
				sub[i] = out[(row*3+i/2)*gw+col*2+i%2]
			}
			fg, bg, mask := fitPair(sub[:])
			fr, fgG, fb := rgbToUint8(fg)
			br, bgG, bb := rgbToUint8(bg)
			if !styled || fr != pFR || fgG != pFG || fb != pFB || br != pBR || bgG != pBG || bb != pBB {
				buf = appendCellSGR(buf, fr, fgG, fb, br, bgG, bb)
				pFR, pFG, pFB, pBR, pBG, pBB = fr, fgG, fb, br, bgG, bb
				styled = true
			}
			buf = utf8.AppendRune(buf, sextantRune(mask))
		}
		if styled {
			buf = append(buf, "\x1b[m"...)
		}
		lines[row] = string(buf)
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
// RenderHalfBlocks) so a heavily downscaled cover stays recognisable.
func sampleGrid(img image.Image, gw, gh int) []rgb {
	src := newPixelSource(img)
	var xRangeBuf [512][2]int
	xRanges := computeXRanges(xRangeBuf[:], gw, src.srcW)

	out := make([]rgb, gw*gh)
	for py := range gh {
		y0 := py * src.srcH / gh
		y1 := max((py+1)*src.srcH/gh, y0+1)
		rowOut := out[py*gw:]
		for px := range gw {
			xr := xRanges[px]
			rs, gs, bs, n := src.sumBox(xr[0], y0, xr[1], y1)
			fn := float64(n)
			rowOut[px] = rgb{float64(rs) / fn, float64(gs) / fn, float64(bs) / fn}
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

// atkinson maps every pixel in-place to the palette, spreading 1/8 of the error
// to each of six strictly forward neighbours in scanline order.
func atkinson(px []rgb, gw, gh int, pal []rgb) []rgb {
	taps := [6][2]int{{1, 0}, {2, 0}, {-1, 1}, {0, 1}, {1, 1}, {0, 2}}
	for y := range gh {
		for x := range gw {
			i := y*gw + x
			orig := px[i]
			q := pal[nearest(orig, pal)]
			px[i] = q
			e0, e1, e2 := (orig[0]-q[0])/8, (orig[1]-q[1])/8, (orig[2]-q[2])/8
			for _, t := range taps {
				nx, ny := x+t[0], y+t[1]
				if nx < 0 || nx >= gw || ny >= gh {
					continue
				}
				j := ny*gw + nx
				px[j][0] += e0
				px[j][1] += e1
				px[j][2] += e2
			}
		}
	}
	return px
}

// fitPair picks the fg/bg pair among the cell's sub-pixel colours that
// represents all of them with the least error, and returns the bitmask of
// sub-pixels drawn in fg.
func fitPair(sub []rgb) (fg, bg rgb, mask uint8) {
	var candBuf [6]rgb
	cand := candBuf[:0]
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

func rgbToUint8(c rgb) (uint8, uint8, uint8) {
	return uint8(min(255, max(0, math.Round(c[0])))),
		uint8(min(255, max(0, math.Round(c[1])))),
		uint8(min(255, max(0, math.Round(c[2]))))
}
