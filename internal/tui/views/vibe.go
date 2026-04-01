package views

import (
	"math/rand"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

type vibeMode struct {
	name   string
	mood   string
	energy int
}

var vibeModes = []vibeMode{
	{"Late Night Coding", "Chill / Focus", 4},
	{"Focus Session", "Deep Work", 5},
	{"Morning Groove", "Energetic / Happy", 7},
	{"Rainy Day Feels", "Melancholy / Cosy", 3},
	{"Party Mode", "Hype / Euphoric", 9},
	{"Study Hall", "Concentrated", 5},
	{"Sunset Drive", "Nostalgic / Smooth", 6},
	{"Midnight Feels", "Introspective", 3},
}

// VibeModel drives the vibe panel (right split of the bottom section).
type VibeModel struct {
	idx    int // index into vibeModes
	energy int // 0-10, overrides mode default
}

func NewVibe() *VibeModel {
	return &VibeModel{energy: vibeModes[0].energy}
}

func (v *VibeModel) Update(msg tea.KeyMsg) {
	switch msg.String() {
	case "+", "=":
		if v.energy < 10 {
			v.energy++
		}
	case "-":
		if v.energy > 0 {
			v.energy--
		}
	case "r":
		v.idx = rand.Intn(len(vibeModes)) //nolint:gosec // non-security random for vibe selection
		v.energy = vibeModes[v.idx].energy
	}
}

// Lines returns the vibe panel content as a slice of exactly h strings,
// each visually at most w chars wide. focused highlights the key hints.
func (v *VibeModel) Lines(w, h, step int, focused bool) []string {
	m := vibeModes[v.idx]
	accent := styles.NowPlayingArtist
	muted := styles.QueueItemMuted
	primary := styles.Playing

	var label string
	if focused {
		label = styles.TabActive.Render("Vibe")
	} else {
		label = styles.Header.Render("Vibe")
	}
	sep := muted.Render(strings.Repeat("─", 5))

	energyBar := primary.Render(strings.Repeat("█", v.energy)) +
		muted.Render(strings.Repeat("░", 10-v.energy))

	thinkFrames := []string{"ʕ•ᴥ•ʔ", "ʕ·ᴥ·ʔ", "ʕ˘ᴥ˘ʔ", "ʕ•̀ᴥ•́ʔ"}
	bear := bearStyle.Render(thinkFrames[(step/10)%len(thinkFrames)])

	var hintStyle lipgloss.Style
	if focused {
		hintStyle = accent
	} else {
		hintStyle = muted
	}

	lines := []string{
		label,
		sep,
		accent.Render("Mode:  ") + lipgloss.NewStyle().Foreground(styles.ColorFg).Render(m.name),
		accent.Render("Energy:") + " " + energyBar,
		accent.Render("Mood:  ") + muted.Render(m.mood),
		"",
		bear + " " + muted.Render("thinking..."),
		"",
		hintStyle.Render("[+]") + muted.Render(" more energy"),
		hintStyle.Render("[-]") + muted.Render(" more chill"),
		hintStyle.Render("[r]") + muted.Render(" regenerate"),
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}
