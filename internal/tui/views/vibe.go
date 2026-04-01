package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// VibeQueryMsg is dispatched as a Cmd when the user submits a vibe description.
// The model handles it: calls the vibe agent, searches the provider, appends
// results to the queue, then calls SetResult().
type VibeQueryMsg struct{ Query string }

type vibeState int

const (
	vibeIdle      vibeState = iota // waiting for user to press v
	vibeInputting                  // text input active
	vibeSearching                  // search in progress
	vibeDone                       // tracks added to queue
	vibeError                      // search returned no results / error
	vibeDiscovery                  // discovery mode is active
)

// DiscoveryInfo carries the discovery-mode display data set by the model.
type DiscoveryInfo struct {
	Active     bool
	SeedArtist string
	SeedTitle  string
	Similarity float64 // 0.0=very different, 1.0=very similar
	Refilling  bool    // background search in progress
}

// VibeModel drives the interactive vibe panel (right split of the bottom area).
type VibeModel struct {
	state     vibeState
	input     textinput.Model
	lastQ     string // last submitted query
	added     int    // tracks added on last successful search
	errStr    string // error message from last search
	discovery DiscoveryInfo
}

func NewVibe() *VibeModel {
	ti := textinput.New()
	ti.Placeholder = "late night coding, gym, rainy day…"
	ti.CharLimit = 80
	ti.Prompt = ""
	return &VibeModel{input: ti}
}

// Focus activates text input so the user can type a vibe description.
// It is a no-op while a search is in progress.
func (v *VibeModel) Focus() {
	if v.state == vibeSearching {
		return
	}
	v.state = vibeInputting
	v.input.SetValue("")
	v.input.Focus()
}

// IsFocused reports whether the vibe panel is capturing keyboard input.
func (v *VibeModel) IsFocused() bool { return v.state == vibeInputting }

// SetSearching transitions the panel to the searching state.
// Called by the model immediately after receiving VibeQueryMsg.
func (v *VibeModel) SetSearching() {
	v.state = vibeSearching
	v.input.Blur()
}

// SetResult transitions to done or error.
// Called by the model after the provider search completes.
func (v *VibeModel) SetResult(n int, err error) {
	if err != nil {
		v.state = vibeError
		v.errStr = err.Error()
	} else {
		if v.discovery.Active {
			v.state = vibeDiscovery
		} else {
			v.state = vibeDone
		}
		v.added = n
	}
}

// SetDiscovery updates the discovery-mode display and switches state accordingly.
func (v *VibeModel) SetDiscovery(info DiscoveryInfo) {
	v.discovery = info
	if info.Active {
		if v.state != vibeSearching {
			v.state = vibeDiscovery
		}
		v.input.Blur()
	} else if v.state == vibeDiscovery {
		v.state = vibeIdle
	}
}

// DiscoveryActive reports whether discovery mode is currently active.
func (v *VibeModel) DiscoveryActive() bool { return v.discovery.Active }

// Update processes keyboard input when the vibe panel is focused (vibeInputting).
// Returns a Cmd that dispatches VibeQueryMsg when the user presses Enter.
// All other key events are forwarded to the bubbles textinput.
func (v *VibeModel) Update(msg tea.Msg) tea.Cmd {
	if v.state != vibeInputting {
		return nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch km.String() {
	case "enter":
		q := strings.TrimSpace(v.input.Value())
		if q == "" {
			return nil
		}
		v.lastQ = q
		v.state = vibeSearching
		v.input.Blur()
		return func() tea.Msg { return VibeQueryMsg{Query: q} }
	case "esc":
		v.state = vibeIdle
		v.input.Blur()
		v.input.SetValue("")
		return nil
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(msg)
	return cmd
}

// similarityBar returns a styled block-progress bar and a label for the similarity value.
// The bar interpolates from blue (adventurous/different) to purple (similar/focused).
func similarityBar(similarity float64, barWidth int) (string, string) {
	filled := min(int(similarity*float64(barWidth)), barWidth)

	// Colour of the filled portion: blue at 0.0 → purple at 1.0
	barColor := styles.LerpColor(styles.ColorProgress, styles.ColorPrimary, similarity)
	barStyle := lipgloss.NewStyle().Foreground(barColor)
	bgStyle := lipgloss.NewStyle().Foreground(styles.ColorSurface)

	bar := barStyle.Render(strings.Repeat("█", filled)) +
		bgStyle.Render(strings.Repeat("░", barWidth-filled))

	var label string
	switch {
	case similarity >= 0.9:
		label = "same artist"
	case similarity >= 0.7:
		label = "similar artists"
	case similarity >= 0.5:
		label = "same genre"
	case similarity >= 0.25:
		label = "exploring"
	default:
		label = "pure discovery"
	}
	return bar, label
}

// Lines returns the vibe panel content as exactly h lines each visually ≤ w chars wide.
func (v *VibeModel) Lines(w, h, step int) []string {
	accent := styles.NowPlayingArtist
	muted := styles.QueueItemMuted
	primary := styles.Playing
	errSt := lipgloss.NewStyle().Foreground(styles.ColorError)

	thinkFrames := []string{"ʕ•ᴥ•ʔ", "ʕ·ᴥ·ʔ", "ʕ˘ᴥ˘ʔ", "ʕ•̀ᴥ•́ʔ"}
	bear := bearStyle.Render(thinkFrames[(step/10)%len(thinkFrames)])

	v.input.Width = max(w-4, 10)

	clip := func(s string, maxLen int) string {
		if maxLen <= 0 {
			maxLen = w - 4
		}
		if len([]rune(s)) > maxLen {
			return string([]rune(s)[:maxLen-1]) + "…"
		}
		return s
	}

	labelTitle := "Vibe"
	if v.state == vibeDiscovery {
		labelTitle = "Discovery"
	}
	label := styles.TabActive.Render(labelTitle)
	sep := muted.Render(strings.Repeat("─", 5))

	var lines []string
	switch v.state {
	case vibeDiscovery:
		d := v.discovery
		barW := max(w-20, 6)
		bar, simLabel := similarityBar(d.Similarity, barW)
		pct := fmt.Sprintf("%.0f%%", d.Similarity*100)

		// Label colour follows the bar gradient.
		labelColor := styles.LerpColor(styles.ColorProgress, styles.ColorPrimary, d.Similarity)
		labelStyle := lipgloss.NewStyle().Foreground(labelColor)

		bearStatus := bear + " "
		if d.Refilling {
			bearStatus += primary.Render("queuing next…")
		} else {
			bearStatus += muted.Render("listening…")
		}

		lines = []string{
			label, sep,
			accent.Render(clip(d.SeedArtist, w-4)),
			muted.Render(clip(d.SeedTitle, w-4)),
			"",
			muted.Render("Similarity") + "  " + bar + "  " + labelStyle.Render(pct),
			muted.Render("           ") + labelStyle.Render(simLabel),
			"",
			bearStatus,
			"",
			accent.Render("+") + muted.Render(" similar") +
				"  " + accent.Render("-") + muted.Render(" different") +
				"  " + accent.Render("d") + muted.Render(" stop"),
		}

	case vibeIdle:
		lines = []string{
			label, sep,
			muted.Render("describe your vibe:"),
			"> " + accent.Render(v.input.Placeholder),
			"",
			bear + " " + muted.Render("press v to start"),
			"",
			muted.Render("or ") + accent.Render("d") + muted.Render(" discovery mode"),
		}

	case vibeInputting:
		lines = []string{
			label, sep,
			accent.Render("describe your vibe:"),
			"> " + v.input.View(),
			"",
			bear + " " + muted.Render("listening…"),
			"",
			accent.Render("Enter") + muted.Render(" search") +
				"  " + accent.Render("esc") + muted.Render(" cancel"),
		}

	case vibeSearching:
		lines = []string{
			label, sep,
			muted.Render(`"` + clip(v.lastQ, 0) + `"`),
			"",
			bear + " " + primary.Render("searching…"),
			"",
			muted.Render("adding tracks to your queue…"),
		}

	case vibeDone:
		suffix := "s"
		if v.added == 1 {
			suffix = ""
		}
		lines = []string{
			label, sep,
			muted.Render(`"` + clip(v.lastQ, 0) + `"`),
			"",
			bear + " " + primary.Render(fmt.Sprintf("✓ added %d track%s", v.added, suffix)),
			"",
			accent.Render("v") + muted.Render(" new vibe") +
				"  " + accent.Render("d") + muted.Render(" discovery"),
		}

	case vibeError:
		lines = []string{
			label, sep,
			muted.Render(`"` + clip(v.lastQ, 0) + `"`),
			"",
			bearStyle.Render("ʕ•̀ᴥ•́ʔ") + " " + errSt.Render("no results"),
			"",
			accent.Render("v") + muted.Render(" try again") +
				"  " + accent.Render("d") + muted.Render(" discovery"),
		}
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}
