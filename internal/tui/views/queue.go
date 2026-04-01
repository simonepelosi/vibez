package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

type queueItem struct {
	track provider.Track
	pos   int
}

func (q queueItem) Title() string {
	return fmt.Sprintf("%d. %s", q.pos, q.track.Title)
}
func (q queueItem) Description() string {
	return fmt.Sprintf("   %s — %s", q.track.Artist, q.track.Album)
}
func (q queueItem) FilterValue() string { return q.track.Title }

type QueueModel struct {
	list   list.Model
	tracks []provider.Track
}

func NewQueue() *QueueModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(styles.ColorPrimary)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(styles.ColorSubtle)

	l := list.New(nil, delegate, 0, 0)
	l.Title = "Queue"
	l.Styles.Title = lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return &QueueModel{list: l}
}

func (m *QueueModel) SetTracks(tracks []provider.Track) {
	m.tracks = tracks
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = queueItem{track: t, pos: i + 1}
	}
	m.list.SetItems(items)
}

func (m *QueueModel) Tracks() []provider.Track { return m.tracks }

// SelectedTrack returns the index and track of the currently highlighted queue
// item, or (-1, nil) if the queue is empty.
func (m *QueueModel) SelectedTrack() (int, *provider.Track) {
	idx := m.list.Index()
	if idx < 0 || idx >= len(m.tracks) {
		return -1, nil
	}
	t := m.tracks[idx]
	return idx, &t
}

func (m *QueueModel) SetSize(w, h int) {
	m.list.SetSize(w, h)
}

func (m *QueueModel) Update(msg tea.KeyMsg) {
	m.list, _ = m.list.Update(msg)
}

func (m *QueueModel) View() string {
	if len(m.tracks) == 0 {
		return "\n" + centerLine(
			styles.QueueItemMuted.Render("Queue is empty. Browse library or search to add tracks."),
			m.list.Width(),
		)
	}
	return m.list.View()
}

func queueTrackLine(t provider.Track, idx, selected int) string {
	num := styles.QueueItemMuted.Render(fmt.Sprintf("%3d. ", idx+1))
	title := t.Title
	if idx == selected {
		title = styles.Selected.Render(title)
	} else {
		title = styles.QueueItem.Render(title)
	}
	artist := styles.QueueItemMuted.Render(" — " + t.Artist)
	return strings.TrimRight(num+title+artist, " ")
}

var _ = queueTrackLine // used as reference; list.Model handles rendering
