package styles

import "github.com/charmbracelet/lipgloss"

const (
	ColorPrimary   = lipgloss.Color("#C678DD")
	ColorSecondary = lipgloss.Color("#98C379")
	ColorMuted     = lipgloss.Color("#5C6370")
	ColorError     = lipgloss.Color("#E06C75")
	ColorFg        = lipgloss.Color("#ABB2BF")
	ColorSubtle    = lipgloss.Color("#61AFEF")
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
			Foreground(ColorFg).
			MarginTop(1)

	NowPlayingArtist = lipgloss.NewStyle().
				Foreground(ColorSubtle)

	NowPlayingAlbum = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Playing = lipgloss.NewStyle().
		Foreground(ColorSecondary).
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
			Foreground(ColorPrimary).
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
			Foreground(ColorPrimary).
			Bold(true)

	SidebarInactive = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Separator = lipgloss.NewStyle().
			Foreground(ColorMuted)

	Header = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true)
)

// GlowPalette is the sequence of colours used for the "now playing" breathing
// animation. It runs from deep purple (dim) to bright lavender (peak) and is
// driven in a ping-pong loop so the title appears to pulse like the Copilot
// "thinking" glow.
var GlowPalette = []lipgloss.Color{
	"#3B0764", // 0 — deepest dim
	"#581C87", // 1
	"#7E22CE", // 2
	"#9333EA", // 3
	"#A855F7", // 4
	"#C084FC", // 5
	"#D8B4FE", // 6
	"#EDE9FE", // 7 — peak brightness
}
