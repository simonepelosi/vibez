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
			Foreground(lipgloss.Color("#E5C07B")) // warm amber

	NowPlayingTitle = lipgloss.NewStyle().
			Italic(true).
			Foreground(ColorFg) // soft gray when paused

	NowPlayingTitlePlaying = lipgloss.NewStyle().
				Italic(true).
				Foreground(lipgloss.Color("#E0D4FF")) // bright lavender when playing

	NowPlayingArtist = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#C678DD")) // violet — replaces boring blue

	NowPlayingAlbum = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Playing = lipgloss.NewStyle().
		Foreground(ColorGlow5)

	Paused = lipgloss.NewStyle().
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

	// Mode indicator styles — distinct background chips like nvim
	ModeNormal     = lipgloss.NewStyle().Background(lipgloss.Color("#98C379")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)
	ModeSearch     = lipgloss.NewStyle().Background(lipgloss.Color("#61AFEF")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)
	ModeCommand    = lipgloss.NewStyle().Background(lipgloss.Color("#E5C07B")).Foreground(lipgloss.Color("#1a1b26")).Bold(true).Padding(0, 1)
	ProgressBar    = lipgloss.NewStyle().Foreground(ColorGlow5)
	ProgressBg     = lipgloss.NewStyle().Foreground(ColorMuted)
	TimeStyle      = lipgloss.NewStyle().Foreground(ColorMuted)
	NowPlayingTime = lipgloss.NewStyle().Foreground(ColorAccent)
	FavoriteActive = lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75"))
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
