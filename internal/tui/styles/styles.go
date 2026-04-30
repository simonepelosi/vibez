package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Color variables — all mutable so Apply(Theme) can swap the palette at startup.
// Initial values match DefaultTheme (Tokyo Night / Catppuccin Mocha blend).
var (
	ColorPrimary   = lipgloss.Color("#C678DD")
	ColorSecondary = lipgloss.Color("#98C379")
	ColorMuted     = lipgloss.Color("#5C6370")
	ColorError     = lipgloss.Color("#E06C75")
	ColorFg        = lipgloss.Color("#ABB2BF")
	ColorSubtle    = lipgloss.Color("#61AFEF")

	ColorBg = lipgloss.Color("#1a1b26")

	ColorLove       = lipgloss.Color("#f38ba8")
	ColorActive     = lipgloss.Color("#a6e3a1")
	ColorProgress   = lipgloss.Color("#89b4fa")
	ColorSurface    = lipgloss.Color("#2a2b3d")
	ColorAccent     = lipgloss.Color("#7aa2f7")
	ColorAccentWarm = lipgloss.Color("#E5C07B") // amber — labels, search, note animations
	ColorBear       = lipgloss.Color("#C4A265") // bear mascot body

	ColorGlow0 = lipgloss.Color("#1e1e2e")
	ColorGlow1 = lipgloss.Color("#2d2b55")
	ColorGlow2 = lipgloss.Color("#4a3f8a")
	ColorGlow3 = lipgloss.Color("#6e57c4")
	ColorGlow4 = lipgloss.Color("#9d7fea")
	ColorGlow5 = lipgloss.Color("#bb9af7")
	ColorGlow6 = lipgloss.Color("#cba6f7")
	ColorGlow7 = lipgloss.Color("#e0d4ff")
)

var (
	TitleBar = lipgloss.NewStyle().
			Italic(true).
			Foreground(ColorPrimary).
			PaddingLeft(1).
			PaddingRight(1)

	ViewName = lipgloss.NewStyle().
			Foreground(ColorMuted).
			PaddingLeft(2)

	StatusBar = lipgloss.NewStyle().
			Foreground(ColorFg).
			PaddingLeft(1).
			PaddingRight(1)

	NowPlayingLabel = lipgloss.NewStyle().
			Italic(true).
			Bold(true).
			Foreground(ColorAccentWarm)

	NowPlayingTitle = lipgloss.NewStyle().
			Italic(true).
			Foreground(ColorFg)

	NowPlayingTitlePlaying = lipgloss.NewStyle().
				Italic(true).
				Bold(true).
				Foreground(ColorGlow7)

	NowPlayingArtist = lipgloss.NewStyle().
				Foreground(ColorPrimary)

	NowPlayingAlbum = lipgloss.NewStyle().
			Foreground(ColorMuted)

	ControlActive = lipgloss.NewStyle().Foreground(ColorActive)

	Playing = lipgloss.NewStyle().
		Foreground(ColorGlow5)

	Paused = lipgloss.NewStyle().
		Foreground(ColorMuted)

	VibingStatus = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Selected = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Italic(true)

	QueueItem = lipgloss.NewStyle().
			Foreground(ColorFg)

	QueueItemMuted = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Border = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorMuted)

	KeyHint = lipgloss.NewStyle().
		Foreground(ColorMuted).
		PaddingLeft(1)

	KeyName = lipgloss.NewStyle().
		Foreground(ColorAccent)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Italic(true).
			PaddingLeft(1)

	ArtworkFrame = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(1, 2).
			Align(lipgloss.Center)

	TabActive = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Underline(true).
			PaddingLeft(1).
			PaddingRight(1)

	TabInactive = lipgloss.NewStyle().
			Foreground(ColorMuted).
			PaddingLeft(1).
			PaddingRight(1)

	Spinner = lipgloss.NewStyle().
		Foreground(ColorGlow4)

	SidebarActive = lipgloss.NewStyle().
			Foreground(ColorGlow5).
			Italic(true)

	SidebarInactive = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Separator = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Header = lipgloss.NewStyle().
		Foreground(ColorAccent)

	ModeNormal  = lipgloss.NewStyle().Background(ColorSecondary).Foreground(ColorBg).Bold(true).Padding(0, 1)
	ModeSearch  = lipgloss.NewStyle().Background(ColorSubtle).Foreground(ColorBg).Bold(true).Padding(0, 1)
	ModeCommand = lipgloss.NewStyle().Background(ColorAccentWarm).Foreground(ColorBg).Bold(true).Padding(0, 1)

	ProgressBar = lipgloss.NewStyle().Foreground(ColorProgress)
	ProgressBg  = lipgloss.NewStyle().Foreground(ColorSurface)

	TimeStyle      = lipgloss.NewStyle().Foreground(ColorMuted)
	NowPlayingTime = lipgloss.NewStyle().Foreground(ColorProgress)

	FavoriteActive = lipgloss.NewStyle().Foreground(ColorLove)

	// Bear mascot styles — updated by Apply().
	BearStyle  = lipgloss.NewStyle().Foreground(ColorBear)
	NoteStyle  = lipgloss.NewStyle().Foreground(ColorAccentWarm)
	SleepStyle = lipgloss.NewStyle().Foreground(ColorAccent).Faint(true)
)

// GlowPalette drives the "now playing" breathing animation, from dark to bright.
var GlowPalette = []color.Color{
	ColorGlow0, // 0 — darkest
	ColorGlow1,
	ColorGlow2,
	ColorGlow3,
	ColorGlow4,
	ColorGlow5,
	ColorGlow6,
	ColorGlow7, // 7 — peak brightness
}

// ProgressGradStops are the colour stops for the progress-bar filled gradient
// (left → right: blue → lavender → rose pink). Updated by Apply().
var ProgressGradStops = []color.Color{
	ColorProgress, // blue
	ColorGlow6,    // lavender
	ColorLove,     // rose pink
}

// Apply replaces every style and color variable with values derived from t.
// It must be called once at program startup, before any TUI model is created.
func Apply(t Theme) {
	// Update color vars.
	ColorPrimary = lipgloss.Color(t.Primary)
	ColorSecondary = lipgloss.Color(t.Secondary)
	ColorMuted = lipgloss.Color(t.Muted)
	ColorError = lipgloss.Color(t.Error)
	ColorFg = lipgloss.Color(t.Fg)
	ColorSubtle = lipgloss.Color(t.Subtle)
	ColorBg = lipgloss.Color(t.Bg)
	ColorLove = lipgloss.Color(t.Love)
	ColorActive = lipgloss.Color(t.Active)
	ColorProgress = lipgloss.Color(t.Progress)
	ColorSurface = lipgloss.Color(t.Surface)
	ColorAccent = lipgloss.Color(t.Accent)
	ColorAccentWarm = lipgloss.Color(t.AccentWarm)
	ColorBear = lipgloss.Color(t.Bear)
	ColorGlow0 = lipgloss.Color(t.GlowPalette[0])
	ColorGlow1 = lipgloss.Color(t.GlowPalette[1])
	ColorGlow2 = lipgloss.Color(t.GlowPalette[2])
	ColorGlow3 = lipgloss.Color(t.GlowPalette[3])
	ColorGlow4 = lipgloss.Color(t.GlowPalette[4])
	ColorGlow5 = lipgloss.Color(t.GlowPalette[5])
	ColorGlow6 = lipgloss.Color(t.GlowPalette[6])
	ColorGlow7 = lipgloss.Color(t.GlowPalette[7])

	// Update glow slices.
	GlowPalette = []color.Color{
		ColorGlow0, ColorGlow1, ColorGlow2, ColorGlow3,
		ColorGlow4, ColorGlow5, ColorGlow6, ColorGlow7,
	}
	ProgressGradStops = []color.Color{ColorProgress, ColorGlow6, ColorLove}

	// Recreate style vars.
	TitleBar = lipgloss.NewStyle().Italic(true).Foreground(ColorPrimary).PaddingLeft(1).PaddingRight(1)
	ViewName = lipgloss.NewStyle().Foreground(ColorMuted).PaddingLeft(2)
	StatusBar = lipgloss.NewStyle().Foreground(ColorFg).PaddingLeft(1).PaddingRight(1)
	NowPlayingLabel = lipgloss.NewStyle().Italic(true).Bold(true).Foreground(ColorAccentWarm)
	NowPlayingTitle = lipgloss.NewStyle().Italic(true).Foreground(ColorFg)
	NowPlayingTitlePlaying = lipgloss.NewStyle().Italic(true).Bold(true).Foreground(ColorGlow7)
	NowPlayingArtist = lipgloss.NewStyle().Foreground(ColorPrimary)
	NowPlayingAlbum = lipgloss.NewStyle().Foreground(ColorMuted)
	ControlActive = lipgloss.NewStyle().Foreground(ColorActive)
	Playing = lipgloss.NewStyle().Foreground(ColorGlow5)
	Paused = lipgloss.NewStyle().Foreground(ColorMuted)
	VibingStatus = lipgloss.NewStyle().Foreground(ColorMuted)
	Selected = lipgloss.NewStyle().Foreground(ColorPrimary).Italic(true)
	QueueItem = lipgloss.NewStyle().Foreground(ColorFg)
	QueueItemMuted = lipgloss.NewStyle().Foreground(ColorMuted)
	Border = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorMuted)
	KeyHint = lipgloss.NewStyle().Foreground(ColorMuted).PaddingLeft(1)
	KeyName = lipgloss.NewStyle().Foreground(ColorAccent)
	ErrorStyle = lipgloss.NewStyle().Foreground(ColorError).Italic(true).PaddingLeft(1)
	ArtworkFrame = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorPrimary).Padding(1, 2).Align(lipgloss.Center)
	TabActive = lipgloss.NewStyle().Foreground(ColorAccent).Underline(true).PaddingLeft(1).PaddingRight(1)
	TabInactive = lipgloss.NewStyle().Foreground(ColorMuted).PaddingLeft(1).PaddingRight(1)
	Spinner = lipgloss.NewStyle().Foreground(ColorGlow4)
	SidebarActive = lipgloss.NewStyle().Foreground(ColorGlow5).Italic(true)
	SidebarInactive = lipgloss.NewStyle().Foreground(ColorMuted)
	Separator = lipgloss.NewStyle().Foreground(ColorMuted)
	Header = lipgloss.NewStyle().Foreground(ColorAccent)
	ModeNormal = lipgloss.NewStyle().Background(ColorSecondary).Foreground(ColorBg).Bold(true).Padding(0, 1)
	ModeSearch = lipgloss.NewStyle().Background(ColorSubtle).Foreground(ColorBg).Bold(true).Padding(0, 1)
	ModeCommand = lipgloss.NewStyle().Background(ColorAccentWarm).Foreground(ColorBg).Bold(true).Padding(0, 1)
	ProgressBar = lipgloss.NewStyle().Foreground(ColorProgress)
	ProgressBg = lipgloss.NewStyle().Foreground(ColorSurface)
	TimeStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	NowPlayingTime = lipgloss.NewStyle().Foreground(ColorProgress)
	FavoriteActive = lipgloss.NewStyle().Foreground(ColorLove)
	BearStyle = lipgloss.NewStyle().Foreground(ColorBear)
	NoteStyle = lipgloss.NewStyle().Foreground(ColorAccentWarm)
	SleepStyle = lipgloss.NewStyle().Foreground(ColorAccent).Faint(true)
}

// LerpColor linearly interpolates between two colours by factor t ∈ [0,1].
// Used to compute gradient colours for dynamic indicators like the similarity bar.
func LerpColor(a, b color.Color, t float64) color.Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	r := uint8(float64(ar>>8) + t*(float64(br>>8)-float64(ar>>8)))
	g := uint8(float64(ag>>8) + t*(float64(bg>>8)-float64(ag>>8)))
	bl := uint8(float64(ab>>8) + t*(float64(bb>>8)-float64(ab>>8)))
	return lipgloss.Color("#" + hex2(r) + hex2(g) + hex2(bl))
}

// ColorHex returns the "#rrggbb" lowercase hex representation of a color.Color.
// Useful for comparing color values against theme hex strings.
func ColorHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	// RGBA() returns 16-bit alpha-premultiplied values (0-65535); >>8 maps to [0,255]. //nolint:gosec
	return "#" + hex2(uint8(r>>8)) + hex2(uint8(g>>8)) + hex2(uint8(b>>8)) //nolint:gosec
}

func hex2(v uint8) string {
	const h = "0123456789abcdef"
	return string([]byte{h[v>>4], h[v&0xf]})
}
