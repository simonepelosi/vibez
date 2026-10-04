package tui

import (
	"image"
	"math"

	"github.com/EdlinOrg/prominentcolor"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// themeFromCover reuses the image clustering library, rather than implementing
// a quantizer. It runs in the artwork download command, outside the UI loop.
func themeFromCover(img image.Image, base styles.Theme) (styles.Theme, bool) {
	if img == nil || img.Bounds().Empty() {
		return base, false
	}
	colors, err := prominentcolor.KmeansWithAll(3, img, prominentcolor.ArgumentNoCropping|prominentcolor.ArgumentAverageMean, 80, nil)
	if err != nil || len(colors) == 0 {
		return base, false
	}
	dominant := colors[0]
	for _, candidate := range colors {
		c := colorful.Color{R: float64(candidate.Color.R) / 255, G: float64(candidate.Color.G) / 255, B: float64(candidate.Color.B) / 255}
		_, s, l := c.Hsl()
		// Prefer a coloured surface over black shadows or white lettering.
		if s >= 0.12 && l >= 0.08 && l <= 0.92 {
			dominant = candidate
			break
		}
	}
	c := colorful.Color{R: float64(dominant.Color.R) / 255, G: float64(dominant.Color.G) / 255, B: float64(dominant.Color.B) / 255}
	hue, sat, _ := c.Hsl()
	sat = math.Min(sat, 0.65)
	tint := func(s, l float64) string { return colorful.Hsl(hue, s, l).Clamped().Hex() }
	accent := tint(sat, 0.68)
	light := tint(sat*0.35, 0.84)
	muted := tint(sat*0.25, 0.57)
	base.Primary, base.Secondary = accent, tint(sat*0.75, 0.74)
	base.Fg, base.Muted = light, muted
	base.Subtle, base.Accent, base.AccentWarm = accent, accent, accent
	base.Active, base.Progress, base.Bear = accent, accent, accent
	base.Surface = tint(sat*0.25, 0.22)
	for i := range base.GlowPalette {
		base.GlowPalette[i] = tint(sat*0.5, 0.40+0.065*float64(i))
	}
	base.ModeNormalBg, base.ModeSearchBg, base.ModeCommandBg = accent, accent, accent
	return base, true
}
