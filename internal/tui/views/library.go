package views

import (
	"context"
	"fmt"

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

type libraryLoadedMsg struct {
	tab       libraryTab
	tracks    []provider.Track
	playlists []provider.Playlist
	err       error
}

type LibraryModel struct {
	provider  provider.Provider
	activeTab libraryTab
	loading   bool
	list      list.Model
	spinner   spinner.Model

	playlists []provider.Playlist
	tracks    []provider.Track

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

	sp := spinner.New()
	sp.Style = styles.Spinner
	sp.Spinner = spinner.Dot

	return &LibraryModel{
		provider: prov,
		list:     l,
		spinner:  sp,
	}
}

func (m *LibraryModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadPlaylists())
}

func (m *LibraryModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.list.SetSize(w, h-3) // reserve space for tabs
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

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
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
						prov := m.provider
						id := item.ID
						return m, func() tea.Msg {
							tracks, err := prov.GetPlaylistTracks(context.Background(), id)
							if err != nil || len(tracks) == 0 {
								return nil
							}
							ids := make([]string, len(tracks))
							for i, t := range tracks {
								ids[i] = t.ID
							}
							return PlayTracksMsg{IDs: ids}
						}
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
	tabs := m.renderTabs()
	if m.loading {
		return tabs + "\n\n  " + m.spinner.View() + " Loading…"
	}
	return tabs + "\n" + m.list.View()
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

func (p playlistItem) Title() string       { return p.Name }
func (p playlistItem) Description() string { return fmt.Sprintf("%d tracks", p.TrackCount) }
func (p playlistItem) FilterValue() string { return p.Name }

type trackListItem struct{ t provider.Track }

func (i trackListItem) Title() string       { return i.t.Title }
func (i trackListItem) Description() string { return fmt.Sprintf("%s — %s", i.t.Artist, i.t.Album) }
func (i trackListItem) FilterValue() string { return i.t.Title + " " + i.t.Artist }
