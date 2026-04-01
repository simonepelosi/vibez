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
)

// VibeModel drives the interactive vibe panel (right split of the bottom area).
type VibeModel struct {
	state  vibeState
	input  textinput.Model
	lastQ  string // last submitted query
	added  int    // tracks added on last successful search
	errStr string // error message from last search
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
		v.state = vibeDone
		v.added = n
	}
}

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

// Lines returns the vibe panel content as exactly h lines each visually ≤ w chars wide.
func (v *VibeModel) Lines(w, h, step int) []string {
	accent := styles.NowPlayingArtist
	muted := styles.QueueItemMuted
	primary := styles.Playing
	errSt := lipgloss.NewStyle().Foreground(styles.ColorError)

	label := styles.TabActive.Render("Vibe")
	sep := muted.Render(strings.Repeat("─", 5))

	thinkFrames := []string{"ʕ•ᴥ•ʔ", "ʕ·ᴥ·ʔ", "ʕ˘ᴥ˘ʔ", "ʕ•̀ᴥ•́ʔ"}
	bear := bearStyle.Render(thinkFrames[(step/10)%len(thinkFrames)])

	v.input.Width = max(w-4, 10)

	clip := func(s string) string {
		if len(s) > w-4 {
			return s[:w-4] + "…"
		}
		return s
	}

	var lines []string
	switch v.state {
	case vibeIdle:
		lines = []string{
			label, sep,
			muted.Render("describe your vibe:"),
			"> " + accent.Render(v.input.Placeholder),
			"",
			bear + " " + muted.Render("press v to start"),
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
			muted.Render(`"` + clip(v.lastQ) + `"`),
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
			muted.Render(`"` + clip(v.lastQ) + `"`),
			"",
			bear + " " + primary.Render(fmt.Sprintf("✓ added %d track%s", v.added, suffix)),
			"",
			accent.Render("v") + muted.Render(" new vibe"),
		}
	case vibeError:
		lines = []string{
			label, sep,
			muted.Render(`"` + clip(v.lastQ) + `"`),
			"",
			bearStyle.Render("ʕ•̀ᴥ•́ʔ") + " " + errSt.Render("no results"),
			"",
			accent.Render("v") + muted.Render(" try again"),
		}
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}
