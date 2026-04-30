package views

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// libraryPane controls which level of the drill-down is visible.
type libraryPane int

const (
	paneList   libraryPane = iota // top-level tab list
	paneTracks                    // tracks inside a selected playlist
)

type libraryLoadedMsg struct {
	playlists []provider.Playlist
	err       error
}

type playlistTracksMsg struct {
	playlist provider.Playlist
	tracks   []provider.Track
	err      error
}

type LibraryModel struct {
	provider provider.Provider
	loading  bool
	loadErr  error
	list     list.Model
	spinner  spinner.Model

	playlists []provider.Playlist

	// Drill-down into a playlist's tracks.
	pane          libraryPane
	drillPlaylist provider.Playlist
	drillTracks   []provider.Track
	drillLoading  bool
	drillErr      error
	drillList     list.Model

	width  int
	height int
}

func NewLibrary(prov provider.Provider) *LibraryModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(styles.ColorPrimary)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(styles.ColorSubtle)

	l := list.New(nil, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(false)

	drill := list.New(nil, delegate, 0, 0)
	drill.SetShowTitle(false)
	drill.SetFilteringEnabled(false)

	sp := spinner.New()
	sp.Style = styles.Spinner
	sp.Spinner = spinner.Dot

	return &LibraryModel{
		provider:  prov,
		list:      l,
		drillList: drill,
		spinner:   sp,
	}
}

func (m *LibraryModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadPlaylists())
}

func (m *LibraryModel) Width() int      { return m.width }
func (m *LibraryModel) Height() int     { return m.height }
func (m *LibraryModel) DrillErr() error { return m.drillErr }
func (m *LibraryModel) LoadErr() error  { return m.loadErr }

func (m *LibraryModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	listH := max(0, h-3)
	m.list.SetSize(w, listH)
	m.drillList.SetSize(w, listH)
}

func (m *LibraryModel) loadPlaylists() tea.Cmd {
	m.loading = true
	prov := m.provider
	return func() tea.Msg {
		playlists, err := prov.GetLibraryPlaylists(context.Background())
		return libraryLoadedMsg{playlists: playlists, err: err}
	}
}

func (m *LibraryModel) loadPlaylistTracks(pl provider.Playlist) tea.Cmd {
	m.drillLoading = true
	prov := m.provider
	return func() tea.Msg {
		tracks, err := prov.GetPlaylistTracks(context.Background(), pl.ID)
		return playlistTracksMsg{playlist: pl, tracks: tracks, err: err}
	}
}

func (m *LibraryModel) Update(msg tea.Msg) (*LibraryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case libraryLoadedMsg:
		m.loading = false
		m.loadErr = msg.err
		if msg.err == nil {
			m.playlists = msg.playlists
			m.refreshList()
		}
		return m, nil

	case playlistTracksMsg:
		m.drillLoading = false
		m.drillErr = msg.err
		if msg.err == nil {
			m.drillTracks = msg.tracks
			items := make([]list.Item, len(msg.tracks))
			for i, t := range msg.tracks {
				items[i] = trackListItem{t}
			}
			m.drillList.SetItems(items)
		}
		return m, nil

	case spinner.TickMsg:
		if m.loading || m.drillLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyPressMsg:
		// Drill-down pane handles its own keys.
		if m.pane == paneTracks {
			switch msg.String() {
			case "esc", "backspace":
				m.pane = paneList
				return m, nil
			case "enter":
				if selected := m.drillList.SelectedItem(); selected != nil {
					if _, ok := selected.(trackListItem); ok {
						// Play from the selected track to end of playlist.
						idx := m.drillList.Index()
						allTracks := m.drillTracks[idx:]
						ids := make([]string, len(allTracks))
						for i, t := range allTracks {
							ids[i] = PlaybackID(t)
						}
						first := allTracks[0]
						tracks := append([]provider.Track{}, allTracks...)
						return m, func() tea.Msg {
							return PlayTracksMsg{IDs: ids, Tracks: tracks, Track: &first}
						}
					}
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.drillList, cmd = m.drillList.Update(msg)
				return m, cmd
			}
		}

		// Top-level pane.
		switch msg.String() {
		case "enter":
			if selected := m.list.SelectedItem(); selected != nil {
				if item, ok := selected.(playlistItem); ok {
					pl := provider.Playlist(item)
					m.drillPlaylist = pl
					m.pane = paneTracks
					m.drillTracks = nil
					m.drillErr = nil
					m.drillList.SetItems(nil)
					return m, tea.Batch(m.spinner.Tick, m.loadPlaylistTracks(pl))
				}
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}
	}

	if m.pane == paneTracks {
		var cmd tea.Cmd
		m.drillList, cmd = m.drillList.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *LibraryModel) refreshList() {
	var items []list.Item
	for _, pl := range m.playlists {
		items = append(items, playlistItem(pl))
	}
	m.list.SetItems(items)
}

func (m *LibraryModel) View() string {
	if m.pane == paneTracks {
		return m.renderDrillView()
	}
	header := m.renderHeader()
	if m.loading {
		return header + "\n\n  " + m.spinner.View() + " Loading…"
	}
	if len(m.playlists) == 0 {
		return header + "\n\n" + centerLine(styles.QueueItemMuted.Render("No playlists found"), m.width)
	}
	return header + "\n" + m.list.View()
}

func (m *LibraryModel) renderDrillView() string {
	name := styles.SidebarActive.Render(m.drillPlaylist.Name)
	hint := styles.QueueItemMuted.Render("  ← esc · enter play")
	header := name + hint + "\n" +
		lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(strings.Repeat("─", m.width))
	if m.drillLoading {
		return header + "\n\n  " + m.spinner.View() + " Loading tracks…"
	}
	if len(m.drillTracks) == 0 {
		return header + "\n\n" + centerLine(styles.QueueItemMuted.Render("No tracks found"), m.width)
	}
	listH := max(0, m.height-3)
	m.drillList.SetSize(m.width, listH)
	return header + "\n" + m.drillList.View()
}

func (m *LibraryModel) renderHeader() string {
	title := styles.TabActive.Render("Playlists")
	return title +
		"\n" + lipgloss.NewStyle().Foreground(styles.ColorMuted).Render("─────────────────────────")
}

// --- list.Item adapters ---

type playlistItem provider.Playlist

func (p playlistItem) Title() string { return p.Name }
func (p playlistItem) Description() string {
	if p.TrackCount > 0 {
		return fmt.Sprintf("%d tracks", p.TrackCount)
	}
	return "playlist · enter to browse"
}
func (p playlistItem) FilterValue() string { return p.Name }

type trackListItem struct{ t provider.Track }

func (i trackListItem) Title() string       { return i.t.Title }
func (i trackListItem) Description() string { return fmt.Sprintf("%s — %s", i.t.Artist, i.t.Album) }
func (i trackListItem) FilterValue() string { return i.t.Title + " " + i.t.Artist }
