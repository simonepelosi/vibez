package styles

import "github.com/charmbracelet/lipgloss"

const (
	ColorPrimary   = lipgloss.Color("#C678DD")
	ColorSecondary = lipgloss.Color("#98C379")
	ColorMuted     = lipgloss.Color("#5C6370")
	ColorError     = lipgloss.Color("#E06C75")
	ColorFg        = lipgloss.Color("#ABB2BF")
	ColorSubtle    = lipgloss.Color("#61AFEF")

	// Copilot-inspired palette.
	ColorBg     = lipgloss.Color("#1a1b26")
	ColorAccent = lipgloss.Color("#7aa2f7")
	ColorGlow0  = lipgloss.Color("#1e1e2e")
	ColorGlow1  = lipgloss.Color("#2d2b55")
	ColorGlow2  = lipgloss.Color("#4a3f8a")
	ColorGlow3  = lipgloss.Color("#6e57c4")
	ColorGlow4  = lipgloss.Color("#9d7fea")
	ColorGlow5  = lipgloss.Color("#bb9af7")
	ColorGlow6  = lipgloss.Color("#cba6f7")
	ColorGlow7  = lipgloss.Color("#e0d4ff")
)

var (
	TitleBar = lipgloss.NewStyle().
			Bold(true).
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

	NowPlayingTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorFg)

	NowPlayingArtist = lipgloss.NewStyle().
				Foreground(ColorAccent)

	NowPlayingAlbum = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Playing = lipgloss.NewStyle().
		Foreground(ColorGlow5).
		Bold(true)

	Paused = lipgloss.NewStyle().
		Foreground(ColorMuted)

	Selected = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

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
		Foreground(ColorPrimary).
		Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true).
			PaddingLeft(1)

	ArtworkFrame = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(1, 2).
			Align(lipgloss.Center)

	TabActive = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			Underline(true).
			PaddingLeft(1).
			PaddingRight(1)

	TabInactive = lipgloss.NewStyle().
			Foreground(ColorMuted).
			PaddingLeft(1).
			PaddingRight(1)

	Spinner = lipgloss.NewStyle().
		Foreground(ColorPrimary)

	SidebarActive = lipgloss.NewStyle().
			Foreground(ColorGlow5).
			Bold(true)

	SidebarInactive = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Separator = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Header = lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)
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
