package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

type feedState int

const (
	feedStateEmpty   feedState = iota // never loaded
	feedStateLoading                  // fetch in progress
	feedStateLoaded                   // showing results
	feedStateError                    // last fetch failed
)

// feedRow is one entry in the flat rendered list.
// Group titles and blank spacers are non-selectable; item rows are selectable.
type feedRow struct {
	header bool
	label  string                       // header text (blank = spacer)
	item   *provider.RecommendationItem // non-nil for selectable rows
}

// FeedModel renders the personalised recommendations panel.
type FeedModel struct {
	state  feedState
	errMsg string
	rows   []feedRow
	cursor int // index into rows[] of the currently highlighted item row
	scroll int
	width  int
	height int
}

func NewFeed() *FeedModel { return &FeedModel{} }

// NeedsLoad reports whether a fetch should be triggered (first open or after error).
func (f *FeedModel) NeedsLoad() bool {
	return f.state == feedStateEmpty || f.state == feedStateError
}

func (f *FeedModel) SetLoading() {
	f.state = feedStateLoading
	f.rows = nil
	f.errMsg = ""
	f.cursor = 0
	f.scroll = 0
}

func (f *FeedModel) SetRecommendations(groups []provider.RecommendationGroup) {
	if len(groups) == 0 {
		f.state = feedStateEmpty
		f.rows = nil
		return
	}
	var rows []feedRow
	for gi, g := range groups {
		rows = append(rows, feedRow{header: true, label: g.Title})
		for i := range g.Items {
			it := &groups[gi].Items[i]
			rows = append(rows, feedRow{item: it})
		}
		rows = append(rows, feedRow{header: true}) // blank spacer between groups
	}
	f.rows = rows
	f.state = feedStateLoaded
	f.cursor = f.advance(-1, 1)
	f.scroll = 0
}

func (f *FeedModel) SetError(err error) {
	f.state = feedStateError
	f.errMsg = err.Error()
}

// SelectedItem returns the currently highlighted recommendation, or nil.
func (f *FeedModel) SelectedItem() *provider.RecommendationItem {
	if f.state != feedStateLoaded || f.cursor < 0 || f.cursor >= len(f.rows) {
		return nil
	}
	return f.rows[f.cursor].item
}

// advance returns the index of the next selectable row in direction dir (+1/-1)
// starting from from. Returns from if no selectable row is found.
func (f *FeedModel) advance(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(f.rows); i += dir {
		if f.rows[i].item != nil {
			return i
		}
	}
	return max(from, 0)
}

func (f *FeedModel) SetSize(w, h int) { f.width = w; f.height = h }

func (f *FeedModel) Update(msg tea.KeyMsg) tea.Cmd {
	if f.state != feedStateLoaded {
		return nil
	}
	switch msg.String() {
	case "j", "down":
		next := f.advance(f.cursor, 1)
		if next != f.cursor {
			f.cursor = next
			if f.cursor >= f.scroll+f.height-2 {
				f.scroll = f.cursor - f.height + 3
			}
		}
	case "k", "up":
		prev := f.advance(f.cursor, -1)
		if prev != f.cursor {
			f.cursor = prev
			if f.cursor < f.scroll {
				f.scroll = f.cursor
			}
		}
	case "g":
		f.cursor = f.advance(-1, 1)
		f.scroll = 0
	case "G":
		f.cursor = f.advance(len(f.rows), -1)
		if s := f.cursor - f.height + 3; s > 0 {
			f.scroll = s
		}
	}
	return nil
}

func (f *FeedModel) View() string {
	muted := styles.QueueItemMuted
	hdr := styles.TabActive
	groupTitle := styles.NowPlayingArtist
	itemStyle := styles.QueueItem
	sel := styles.Playing

	sep := muted.Render(strings.Repeat("─", 5))

	switch f.state {
	case feedStateLoading:
		return hdr.Render("Feed") + "\n" + sep + "\n\n" + muted.Render("loading recommendations…")
	case feedStateEmpty:
		return hdr.Render("Feed") + "\n" + sep + "\n\n" + muted.Render("no recommendations") + "\n\n" +
			muted.Render("press ") + styles.KeyName.Render("r") + muted.Render(" to reload")
	case feedStateError:
		return hdr.Render("Feed") + "\n" + sep + "\n\n" + muted.Render("could not load feed") + "\n\n" +
			muted.Render("press ") + styles.KeyName.Render("r") + muted.Render(" to try again")
	}

	var sb strings.Builder
	sb.WriteString(hdr.Render("Feed") + "\n")
	sb.WriteString(sep + "\n")

	visible := max(f.height-2, 1)
	if f.scroll < 0 {
		f.scroll = 0
	}
	end := min(f.scroll+visible, len(f.rows))
	for i := f.scroll; i < end; i++ {
		row := f.rows[i]
		if row.header {
			if row.label == "" {
				sb.WriteByte('\n')
			} else {
				sb.WriteString(groupTitle.Render(row.label) + "\n")
			}
			continue
		}
		cursor := "  "
		titleSt := itemStyle
		subSt := muted
		if i == f.cursor {
			cursor = sel.Render("▶ ")
			titleSt = sel
			subSt = sel
		}
		kindTag := muted.Render(fmt.Sprintf(" [%s]", row.item.Kind))
		sb.WriteString(cursor + titleSt.Render(row.item.Title) +
			"  " + subSt.Render(row.item.Subtitle) + kindTag + "\n")
	}
	return sb.String()
}
