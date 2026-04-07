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
	vibeIdle            vibeState = iota // waiting for user to press v
	vibeInputting                        // text input active
	vibeSearching                        // search in progress
	vibeDone                             // tracks added to queue
	vibeError                            // search returned no results / error
	vibeDiscovery                        // discovery mode is active
	vibeDiscoveryPicker                  // metric selection picker is open
)

// DiscoveryInfo carries the discovery-mode display data set by the model.
type DiscoveryInfo struct {
	Active     bool
	SeedArtist string
	SeedTitle  string
	Similarity float64 // 0.0=very different, 1.0=very similar
	Refilling  bool    // background search in progress
	AutoMode   bool    // true = continuous auto-refill; false = one-shot
	Count      int     // songs per discovery cycle
}

// DiscoveryMetricSelectedMsg is dispatched when the user picks a similarity
// level in the discovery picker. The model handles it to store the selection.
type DiscoveryMetricSelectedMsg struct{ Similarity float64 }

// discoveryOption is one entry in the metric picker.
type discoveryOption struct {
	label      string
	similarity float64
}

var discoveryOptions = []discoveryOption{
	{"same artist", 0.95},
	{"similar artists", 0.75},
	{"same genre", 0.50},
	{"exploring", 0.30},
	{"pure discovery", 0.10},
}

// closestOptionIdx returns the index of the option whose similarity is nearest
// to the given value — used to pre-select the right row when opening the picker.
func closestOptionIdx(similarity float64) int {
	best, bestDiff := 0, 1.0
	for i, opt := range discoveryOptions {
		d := opt.similarity - similarity
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			bestDiff = d
			best = i
		}
	}
	return best
}

// VibeModel drives the interactive vibe panel (right split of the bottom area).
type VibeModel struct {
	state     vibeState
	input     textinput.Model
	lastQ     string // last submitted query
	added     int    // tracks added on last successful search
	errStr    string // error message from last search
	discovery DiscoveryInfo
	pickerIdx int // selected row in the metric picker
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

// ShowPicker opens the similarity metric picker, pre-selecting the option
// closest to currentSimilarity. No-op while a search is in progress.
func (v *VibeModel) ShowPicker(currentSimilarity float64) {
	if v.state == vibeSearching {
		return
	}
	v.pickerIdx = closestOptionIdx(currentSimilarity)
	v.state = vibeDiscoveryPicker
	v.input.Blur()
}

// HidePicker closes the picker and returns to idle. No-op if not in picker state.
func (v *VibeModel) HidePicker() {
	if v.state == vibeDiscoveryPicker {
		v.state = vibeIdle
	}
}

// PickerActive reports whether the metric picker is currently open.
func (v *VibeModel) PickerActive() bool { return v.state == vibeDiscoveryPicker }

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
		if v.state != vibeSearching && v.state != vibeDiscoveryPicker {
			v.state = vibeDiscovery
		}
		v.input.Blur()
	} else if v.state == vibeDiscovery {
		v.state = vibeIdle
	}
}

// DiscoveryActive reports whether discovery mode is currently active.
func (v *VibeModel) DiscoveryActive() bool { return v.discovery.Active }

// Update processes keyboard input when the vibe panel is focused (vibeInputting)
// or when the metric picker is open (vibeDiscoveryPicker).
// Returns a Cmd that dispatches VibeQueryMsg or DiscoveryMetricSelectedMsg.
// All other key events are forwarded to the bubbles textinput.
func (v *VibeModel) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch v.state {
	case vibeInputting:
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

	case vibeDiscoveryPicker:
		switch km.String() {
		case "up", "k":
			if v.pickerIdx > 0 {
				v.pickerIdx--
			}
		case "down", "j":
			if v.pickerIdx < len(discoveryOptions)-1 {
				v.pickerIdx++
			}
		case "enter":
			opt := discoveryOptions[v.pickerIdx]
			v.state = vibeIdle
			return func() tea.Msg { return DiscoveryMetricSelectedMsg{Similarity: opt.similarity} }
		case "d", "esc":
			v.state = vibeIdle
		}
	}
	return nil
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
	if v.state == vibeDiscovery || v.state == vibeDiscoveryPicker {
		labelTitle = "Discovery"
	}
	label := styles.TabActive.Render(labelTitle)
	sep := muted.Render(strings.Repeat("─", 5))

	var lines []string
	switch v.state {
	case vibeDiscoveryPicker:
		lines = []string{label, sep, accent.Render("select metric:"), ""}
		for i, opt := range discoveryOptions {
			prefix := "  "
			nameStyle := muted
			if i == v.pickerIdx {
				prefix = accent.Render("▶ ")
				nameStyle = primary
			}
			lines = append(lines, prefix+nameStyle.Render(opt.label))
		}
		lines = append(lines,
			"",
			accent.Render("↑↓")+muted.Render(" navigate")+
				"  "+accent.Render("Enter")+muted.Render(" select")+
				"  "+accent.Render("esc")+muted.Render(" cancel"),
		)

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

		var modeStr string
		if d.AutoMode {
			modeStr = muted.Render("Mode  ") + accent.Render("auto")
		} else {
			modeStr = muted.Render("Mode  ") + accent.Render(fmt.Sprintf("%d song", d.Count))
			if d.Count != 1 {
				modeStr += accent.Render("s")
			}
		}

		lines = []string{
			label, sep,
			accent.Render(clip(d.SeedArtist, w-4)),
			muted.Render(clip(d.SeedTitle, w-4)),
			"",
			muted.Render("Metric") + "  " + bar + "  " + labelStyle.Render(pct),
			muted.Render("       ") + labelStyle.Render(simLabel),
			modeStr,
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
			accent.Render("d") + muted.Render(" set metric  ") + accent.Render(":discover") + muted.Render(" start"),
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
				"  " + accent.Render("d") + muted.Render(" set metric"),
		}

	case vibeError:
		lines = []string{
			label, sep,
			muted.Render(`"` + clip(v.lastQ, 0) + `"`),
			"",
			bearStyle.Render("ʕ•̀ᴥ•́ʔ") + " " + errSt.Render("no results"),
			"",
			accent.Render("v") + muted.Render(" try again") +
				"  " + accent.Render("d") + muted.Render(" set metric"),
		}
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}
