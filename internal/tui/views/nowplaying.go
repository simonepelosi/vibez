package views

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// RenderGlowTitle renders each rune with a colour based on how far the
// "bright spot" has swept past it. The spot starts before the first char,
// sweeps right, exits after the last char, then the cycle restarts.
func RenderGlowTitle(title string, glowStep int) string {
	n := utf8.RuneCountInString(title)
	if n == 0 {
		return ""
	}
	palette := styles.GlowPalette
	pLen := len(palette)
	if pLen == 0 {
		return title
	}

	var palBuf [16][3]uint8
	var palRGB [][3]uint8
	if pLen <= len(palBuf) {
		palRGB = palBuf[:pLen]
	} else {
		palRGB = make([][3]uint8, pLen)
	}
	for i, c := range palette {
		palRGB[i][0], palRGB[i][1], palRGB[i][2] = colorToRGB(c)
	}

	// Sweep position within cycle [0, n+pLen). Consecutive runes with the
	// same palette index share one SGR span.
	pos := glowStep % (n + pLen)
	buf := make([]byte, 0, len(title)+n*6)
	curIdx := -1
	i := 0
	for _, r := range title {
		dist := pos - i
		idx := 0
		if dist >= 0 && dist < pLen {
			idx = pLen - 1 - dist
		}
		if idx != curIdx {
			if curIdx >= 0 {
				buf = append(buf, "\x1b[m"...)
			}
			curIdx = idx
			rgb := palRGB[idx]
			buf = append(buf, "\x1b[3;38;2;"...)
			buf = strconv.AppendUint(buf, uint64(rgb[0]), 10)
			buf = append(buf, ';')
			buf = strconv.AppendUint(buf, uint64(rgb[1]), 10)
			buf = append(buf, ';')
			buf = strconv.AppendUint(buf, uint64(rgb[2]), 10)
			buf = append(buf, 'm')
		}
		buf = utf8.AppendRune(buf, r)
		i++
	}
	if curIdx >= 0 {
		buf = append(buf, "\x1b[m"...)
	}
	return string(buf)
}

func colorToRGB(c color.Color) (uint8, uint8, uint8) {
	if c == nil {
		return 0, 0, 0
	}
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8) //nolint:gosec // 16-bit RGBA >> 8 fits uint8
}

// FormatDuration formats a duration as m:ss. Exported for use in model footer.
func FormatDuration(d time.Duration) string {
	secs := int(d.Seconds())
	mins := secs / 60
	secs %= 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// wavePattern is the repeating 4-char zigzag used for the progress bar wave.
var wavePattern = [4]string{"╱", "╱", "╲", "╲"}

// RenderProgressBar renders a static flat zigzag (╱╱╲╲) progress bar.
// The filled portion is coloured with a blue→lavender→pink gradient;
// the remaining portion uses the muted surface colour with the same zigzag
// so the pattern reads as one continuous line.
func RenderProgressBar(pos, dur time.Duration, width int) string {
	if width <= 0 {
		return ""
	}
	ratio := 0.0
	if dur > 0 {
		ratio = float64(pos) / float64(dur)
		if ratio > 1 {
			ratio = 1
		}
	}
	filled := int(ratio * float64(width))

	var buf []byte
	if filled > 0 {
		buf = make([]byte, 0, filled*22+32)
		stops := styles.ProgressGradStops
		var stopBuf [8][3]float64
		var stopRGB [][3]float64
		if len(stops) <= len(stopBuf) {
			stopRGB = stopBuf[:len(stops)]
		} else {
			stopRGB = make([][3]float64, len(stops))
		}
		for i, c := range stops {
			sr, sg, sb := colorToRGB(c)
			stopRGB[i] = [3]float64{float64(sr), float64(sg), float64(sb)}
		}

		styled := false
		var prevR, prevG, prevB uint8
		for i := range filled {
			r, g, b := gradientRGBAt(i, filled, stopRGB)
			if !styled || r != prevR || g != prevG || b != prevB {
				buf = append(buf, "\x1b[38;2;"...)
				buf = strconv.AppendUint(buf, uint64(r), 10)
				buf = append(buf, ';')
				buf = strconv.AppendUint(buf, uint64(g), 10)
				buf = append(buf, ';')
				buf = strconv.AppendUint(buf, uint64(b), 10)
				buf = append(buf, 'm')
				prevR, prevG, prevB = r, g, b
				styled = true
			}
			buf = append(buf, wavePattern[i&3]...)
		}
		if styled {
			buf = append(buf, "\x1b[m"...)
		}
	}

	unfilled := width - filled
	if unfilled <= 0 {
		return string(buf)
	}

	var uBuf [512]byte
	var rawUnfilled []byte
	if unfilled*3 <= len(uBuf) {
		rawUnfilled = uBuf[:0]
	} else {
		rawUnfilled = make([]byte, 0, unfilled*3)
	}
	for i := filled; i < width; i++ {
		rawUnfilled = append(rawUnfilled, wavePattern[i&3]...)
	}
	bgStr := styles.ProgressBg.Render(string(rawUnfilled))
	if len(buf) == 0 {
		return bgStr
	}
	buf = append(buf, bgStr...)
	return string(buf)
}

func gradientRGBAt(i, total int, stops [][3]float64) (uint8, uint8, uint8) {
	if len(stops) == 0 {
		return 0, 0, 0
	}
	if total <= 1 || len(stops) < 2 {
		return uint8(stops[0][0]), uint8(stops[0][1]), uint8(stops[0][2])
	}
	segments := len(stops) - 1
	t := float64(i) / float64(total-1)
	seg := int(t * float64(segments))
	if seg >= segments {
		last := stops[len(stops)-1]
		return uint8(last[0]), uint8(last[1]), uint8(last[2])
	}
	local := t*float64(segments) - float64(seg)
	a, b := stops[seg], stops[seg+1]
	r := uint8(a[0] + local*(b[0]-a[0]))
	g := uint8(a[1] + local*(b[1]-a[1]))
	bl := uint8(a[2] + local*(b[2]-a[2]))
	return r, g, bl
}

func centerLine(s string, width int) string {
	sw := lipgloss.Width(s)
	pad := max(0, (width-sw)/2)
	return strings.Repeat(" ", pad) + s
}
