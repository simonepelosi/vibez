package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

type libraryTab int

const (
	tabPlaylists libraryTab = iota
	tabAlbums
	tabTracks
)

// libraryPane controls which level of the drill-down is visible.
type libraryPane int

const (
	paneList   libraryPane = iota // top-level tab list
	paneTracks                    // tracks inside a selected playlist
)

type libraryLoadedMsg struct {
	tab       libraryTab
	tracks    []provider.Track
	playlists []provider.Playlist
	err       error
}

type playlistTracksMsg struct {
	playlist provider.Playlist
	tracks   []provider.Track
	err      error
}

type LibraryModel struct {
	provider  provider.Provider
	activeTab libraryTab
	loading   bool
	list      list.Model
	spinner   spinner.Model

	playlists []provider.Playlist
	tracks    []provider.Track

	// Drill-down into a playlist's tracks.
	pane          libraryPane
	drillPlaylist provider.Playlist
	drillTracks   []provider.Track
	drillLoading  bool
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
	l.SetFilteringEnabled(true)

	drill := list.New(nil, delegate, 0, 0)
	drill.SetShowTitle(false)
	drill.SetFilteringEnabled(true)

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
		return libraryLoadedMsg{tab: tabPlaylists, playlists: playlists, err: err}
	}
}

func (m *LibraryModel) loadTracks() tea.Cmd {
	m.loading = true
	prov := m.provider
	return func() tea.Msg {
		tracks, err := prov.GetLibraryTracks(context.Background())
		return libraryLoadedMsg{tab: tabTracks, tracks: tracks, err: err}
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
		if msg.err == nil {
			switch msg.tab {
			case tabPlaylists:
				m.playlists = msg.playlists
			case tabTracks:
				m.tracks = msg.tracks
			}
			m.refreshList()
		}
		return m, nil

	case playlistTracksMsg:
		m.drillLoading = false
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

	case tea.KeyMsg:
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
						ids := make([]string, 0, len(m.drillTracks)-idx)
						for i := idx; i < len(m.drillTracks); i++ {
							ids = append(ids, m.drillTracks[i].ID)
						}
						return m, func() tea.Msg { return PlayTracksMsg{IDs: ids} }
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
		case "tab":
			m.activeTab = (m.activeTab + 1) % 3
			if m.activeTab == tabTracks && m.tracks == nil {
				return m, m.loadTracks()
			}
			m.refreshList()
			return m, nil
		case "enter":
			if selected := m.list.SelectedItem(); selected != nil {
				switch m.activeTab {
				case tabTracks:
					if item, ok := selected.(trackListItem); ok {
						return m, func() tea.Msg {
							return PlayTracksMsg{IDs: []string{item.t.ID}}
						}
					}
				case tabPlaylists:
					if item, ok := selected.(playlistItem); ok {
						pl := provider.Playlist(item)
						m.drillPlaylist = pl
						m.pane = paneTracks
						m.drillTracks = nil
						m.drillList.SetItems(nil)
						return m, tea.Batch(m.spinner.Tick, m.loadPlaylistTracks(pl))
					}
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
	switch m.activeTab {
	case tabPlaylists:
		for _, pl := range m.playlists {
			items = append(items, playlistItem(pl))
		}
	case tabTracks:
		for _, t := range m.tracks {
			items = append(items, trackListItem{t})
		}
	}
	m.list.SetItems(items)
}

func (m *LibraryModel) View() string {
	if m.pane == paneTracks {
		return m.renderDrillView()
	}
	tabs := m.renderTabs()
	if m.loading {
		return tabs + "\n\n  " + m.spinner.View() + " Loading…"
	}
	return tabs + "\n" + m.list.View()
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
	return header + "\n" + m.drillList.View()
}

func (m *LibraryModel) renderTabs() string {
	tabLabels := []string{"Playlists", "Albums", "Tracks"}
	rendered := make([]string, len(tabLabels))
	for i, label := range tabLabels {
		if libraryTab(i) == m.activeTab {
			rendered[i] = styles.TabActive.Render(label)
		} else {
			rendered[i] = styles.TabInactive.Render(label)
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...) +
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
