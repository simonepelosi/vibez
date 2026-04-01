package styles

import "github.com/charmbracelet/lipgloss"

const (
	ColorPrimary   = lipgloss.Color("#C678DD")
	ColorSecondary = lipgloss.Color("#98C379")
	ColorMuted     = lipgloss.Color("#5C6370")
	ColorError     = lipgloss.Color("#E06C75")
	ColorFg        = lipgloss.Color("#ABB2BF")
	ColorSubtle    = lipgloss.Color("#61AFEF")

	// Core theme — Tokyo Night base
	ColorBg = lipgloss.Color("#1a1b26")

	// Catppuccin Mocha semantic accents
	ColorLove     = lipgloss.Color("#f38ba8") // rose pink — loved / favourited
	ColorActive   = lipgloss.Color("#a6e3a1") // mint green — on/active controls
	ColorProgress = lipgloss.Color("#89b4fa") // cornflower blue — progress & time
	ColorSurface  = lipgloss.Color("#2a2b3d") // dim surface — unfilled bars

	// Blue key-binding accent (unchanged)
	ColorAccent = lipgloss.Color("#7aa2f7")

	// Glow sweep — dark purple → bright lavender (used for title & bear animation)
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
			Foreground(lipgloss.Color("#E5C07B")) // warm amber

	NowPlayingTitle = lipgloss.NewStyle().
			Italic(true).
			Foreground(ColorFg) // soft gray when paused

	NowPlayingTitlePlaying = lipgloss.NewStyle().
				Italic(true).
				Bold(true).
				Foreground(lipgloss.Color("#E0D4FF")) // bright lavender when playing

	NowPlayingArtist = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#C678DD")) // violet

	NowPlayingAlbum = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6c7086")) // slightly brighter than raw muted

	// ControlActive is used for controls that are in their ON state
	// (▶ playing, ↺ repeat on, ⇄ shuffle on). Green universally reads as "active".
	ControlActive = lipgloss.NewStyle().Foreground(ColorActive)

	Playing = lipgloss.NewStyle().
		Foreground(ColorGlow5) // lavender — status text, vibe panel results

	Paused = lipgloss.NewStyle().
		Foreground(ColorMuted)

	VibingStatus = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#585b70")) // slightly warmer muted

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

	// Mode indicator chips — distinct background blocks like nvim mode line
	ModeNormal  = lipgloss.NewStyle().Background(lipgloss.Color("#98C379")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)
	ModeSearch  = lipgloss.NewStyle().Background(lipgloss.Color("#61AFEF")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)
	ModeCommand = lipgloss.NewStyle().Background(lipgloss.Color("#E5C07B")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)

	// Progress bar — blue filled, dim surface unfilled
	ProgressBar = lipgloss.NewStyle().Foreground(ColorProgress)
	ProgressBg  = lipgloss.NewStyle().Foreground(ColorSurface)

	TimeStyle      = lipgloss.NewStyle().Foreground(ColorMuted)
	NowPlayingTime = lipgloss.NewStyle().Foreground(ColorProgress) // matches progress bar

	// FavoriteActive — rose pink, clearly distinct from ColorError (red)
	FavoriteActive = lipgloss.NewStyle().Foreground(ColorLove)
)

// GlowPalette drives the "now playing" breathing animation, from dark to bright.
var GlowPalette = []lipgloss.Color{
	ColorGlow0, // 0 — darkest
	ColorGlow1,
	ColorGlow2,
	ColorGlow3,
	ColorGlow4,
	ColorGlow5,
	ColorGlow6,
	ColorGlow7, // 7 — peak brightness
}

// LerpColor linearly interpolates between two hex colours by factor t ∈ [0,1].
// Used to compute gradient colours for dynamic indicators like the similarity bar.
func LerpColor(a, b lipgloss.Color, t float64) lipgloss.Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab := hexRGB(string(a))
	br, bg, bb := hexRGB(string(b))
	r := uint8(float64(ar) + t*(float64(br)-float64(ar)))
	g := uint8(float64(ag) + t*(float64(bg)-float64(ag)))
	bl := uint8(float64(ab) + t*(float64(bb)-float64(ab)))
	return lipgloss.Color(lipgloss.Color("#" + hex2(r) + hex2(g) + hex2(bl)))
}

func hexRGB(c string) (r, g, b uint8) {
	c = c[1:] // strip '#'
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	var v uint32
	for _, ch := range []byte(c) {
		v <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			v |= uint32(ch - '0')
		case ch >= 'a' && ch <= 'f':
			v |= uint32(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			v |= uint32(ch-'A') + 10
		}
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

func hex2(v uint8) string {
	const h = "0123456789abcdef"
	return string([]byte{h[v>>4], h[v&0xf]})
}
