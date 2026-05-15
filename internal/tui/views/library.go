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

type libraryPane int

const (
	paneSections libraryPane = iota
	paneItems
	paneTracks
	paneList = paneItems
)

type librarySection int

const (
	sectionSongs librarySection = iota
	sectionAlbums
	sectionArtists
	sectionPlaylists
)

type libraryLoadedMsg struct {
	playlists []provider.Playlist
	err       error
}

type libraryTracksLoadedMsg struct {
	tracks []provider.Track
	err    error
}

type playlistTracksMsg struct {
	playlist provider.Playlist
	tracks   []provider.Track
	err      error
}

type trackGroup struct {
	title  string
	desc   string
	tracks []provider.Track
}

type LibraryModel struct {
	provider provider.Provider
	loading  bool
	loadErr  error
	list     list.Model
	spinner  spinner.Model

	pane            libraryPane
	selectedSection librarySection
	libraryTracks   []provider.Track
	tracksLoaded    bool
	playlists       []provider.Playlist
	playlistsLoaded bool
	albums          []trackGroup
	artists         []trackGroup

	drillTitle     string
	drillPlaylist  provider.Playlist
	drillTracks    []provider.Track
	drillLoading   bool
	drillErr       error
	drillList      list.Model
	tracksBackPane libraryPane

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
	m := &LibraryModel{provider: prov, list: l, drillList: drill, spinner: sp, pane: paneSections}
	m.showSections()
	return m
}

func (m *LibraryModel) Init() tea.Cmd   { return m.spinner.Tick }
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

func (m *LibraryModel) loadLibraryTracks() tea.Cmd {
	m.loading = true
	prov := m.provider
	return func() tea.Msg {
		tracks, err := prov.GetLibraryTracks(context.Background())
		return libraryTracksLoadedMsg{tracks: tracks, err: err}
	}
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
	case libraryTracksLoadedMsg:
		m.loading = false
		m.loadErr = msg.err
		if msg.err == nil {
			m.libraryTracks = append([]provider.Track{}, msg.tracks...)
			m.tracksLoaded = true
			m.routeLoadedLibraryTracks()
		}
		return m, nil
	case libraryLoadedMsg:
		m.loading = false
		m.loadErr = msg.err
		if msg.err == nil {
			m.playlists = append([]provider.Playlist{}, msg.playlists...)
			m.playlistsLoaded = true
			m.showPlaylists()
		}
		return m, nil
	case playlistTracksMsg:
		m.drillLoading = false
		m.drillErr = msg.err
		m.drillPlaylist = msg.playlist
		if msg.err == nil {
			m.showTrackPane(msg.playlist.Name, msg.tracks, paneItems)
		}
		return m, nil
	case spinner.TickMsg:
		if m.loading || m.drillLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m.updateActiveList(msg)
}

func (m *LibraryModel) handleKey(msg tea.KeyPressMsg) (*LibraryModel, tea.Cmd) {
	switch m.pane {
	case paneSections:
		if msg.String() == "enter" {
			if item, ok := m.list.SelectedItem().(sectionItem); ok {
				m.selectedSection = item.section
				return m.openSelectedSection()
			}
			return m, nil
		}
	case paneItems:
		switch msg.String() {
		case "esc", "backspace":
			m.pane = paneSections
			m.showSections()
			return m, nil
		case "enter":
			return m.openSelectedItem()
		}
	case paneTracks:
		switch msg.String() {
		case "esc", "backspace":
			if m.tracksBackPane == paneSections && m.drillTitle == "" {
				m.pane = paneList
			} else {
				m.pane = m.tracksBackPane
			}
			return m, nil
		case "enter":
			return m, m.playTracksFromSelection()
		}
	}
	return m.updateActiveList(msg)
}

func (m *LibraryModel) updateActiveList(msg tea.Msg) (*LibraryModel, tea.Cmd) {
	var cmd tea.Cmd
	if m.pane == paneTracks {
		m.drillList, cmd = m.drillList.Update(msg)
		return m, cmd
	}
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *LibraryModel) openSelectedSection() (*LibraryModel, tea.Cmd) {
	switch m.selectedSection {
	case sectionSongs, sectionAlbums, sectionArtists:
		if !m.tracksLoaded {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.loadLibraryTracks())
		}
		m.routeLoadedLibraryTracks()
		return m, nil
	case sectionPlaylists:
		if !m.playlistsLoaded {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.loadPlaylists())
		}
		m.showPlaylists()
		return m, nil
	default:
		panic("unknown library section")
	}
}

func (m *LibraryModel) routeLoadedLibraryTracks() {
	switch m.selectedSection {
	case sectionSongs:
		m.showTrackPane("Songs", m.libraryTracks, paneSections)
	case sectionAlbums:
		m.albums = buildLibraryAlbums(m.libraryTracks)
		m.showGroups(m.albums)
	case sectionArtists:
		m.artists = buildLibraryArtists(m.libraryTracks)
		m.showGroups(m.artists)
	case sectionPlaylists:
		m.showPlaylists()
	default:
		panic("unknown library section")
	}
}

func (m *LibraryModel) openSelectedItem() (*LibraryModel, tea.Cmd) {
	switch item := m.list.SelectedItem().(type) {
	case albumItem:
		m.showTrackPane(item.group.title, item.group.tracks, paneItems)
	case artistItem:
		m.showTrackPane(item.group.title, item.group.tracks, paneItems)
	case playlistItem:
		pl := provider.Playlist(item)
		m.drillPlaylist = pl
		m.drillTitle = pl.Name
		m.pane = paneTracks
		m.tracksBackPane = paneItems
		m.drillTracks = nil
		m.drillErr = nil
		m.drillList.SetItems(nil)
		return m, tea.Batch(m.spinner.Tick, m.loadPlaylistTracks(pl))
	}
	return m, nil
}

func (m *LibraryModel) playTracksFromSelection() tea.Cmd {
	if selected := m.drillList.SelectedItem(); selected == nil {
		return nil
	}
	idx := m.drillList.Index()
	if idx < 0 || idx >= len(m.drillTracks) {
		return nil
	}
	allTracks := m.drillTracks[idx:]
	ids := make([]string, len(allTracks))
	for i, t := range allTracks {
		ids[i] = PlaybackID(t)
	}
	first := allTracks[0]
	tracks := append([]provider.Track{}, allTracks...)
	return func() tea.Msg { return PlayTracksMsg{IDs: ids, Tracks: tracks, Track: &first} }
}

func (m *LibraryModel) showSections() {
	items := []list.Item{sectionItem{sectionSongs}, sectionItem{sectionAlbums}, sectionItem{sectionArtists}, sectionItem{sectionPlaylists}}
	m.list.SetItems(items)
	m.list.Select(0)
}

func (m *LibraryModel) showPlaylists() {
	m.pane = paneItems
	items := make([]list.Item, len(m.playlists))
	for i, pl := range m.playlists {
		items[i] = playlistItem(pl)
	}
	m.list.SetItems(items)
	m.list.Select(0)
}

func (m *LibraryModel) showGroups(groups []trackGroup) {
	m.pane = paneItems
	items := make([]list.Item, len(groups))
	for i, group := range groups {
		if m.selectedSection == sectionAlbums {
			items[i] = albumItem{group}
		} else {
			items[i] = artistItem{group}
		}
	}
	m.list.SetItems(items)
	m.list.Select(0)
}

func (m *LibraryModel) showTrackPane(title string, tracks []provider.Track, back libraryPane) {
	m.pane = paneTracks
	m.tracksBackPane = back
	m.drillTitle = title
	m.drillTracks = append([]provider.Track{}, tracks...)
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = trackListItem{t}
	}
	m.drillList.SetItems(items)
	m.drillList.Select(0)
}

func (m *LibraryModel) View() string {
	if m.pane == paneTracks {
		return m.renderDrillView()
	}
	header := m.renderHeader()
	if m.loading {
		return header + "\n\n  " + m.spinner.View() + " " + m.loadingText()
	}
	if len(m.list.Items()) == 0 {
		return header + "\n\n" + centerLine(styles.QueueItemMuted.Render(m.emptyText()), m.width)
	}
	return header + "\n" + m.list.View()
}

func (m *LibraryModel) renderDrillView() string {
	name := styles.SidebarActive.Render(m.drillTitle)
	if m.drillTitle == "" {
		name = styles.SidebarActive.Render("Tracks")
	}
	hint := styles.QueueItemMuted.Render("  ← esc · enter play")
	header := name + hint + "\n" + lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(strings.Repeat("─", m.width))
	if m.drillLoading {
		return header + "\n\n  " + m.spinner.View() + " Loading tracks…"
	}
	if len(m.drillTracks) == 0 {
		return header + "\n\n" + centerLine(styles.QueueItemMuted.Render("No tracks found"), m.width)
	}
	m.drillList.SetSize(m.width, max(0, m.height-3))
	return header + "\n" + m.drillList.View()
}

func (m *LibraryModel) renderHeader() string {
	title := "Library"
	if m.pane == paneItems {
		title = sectionTitle(m.selectedSection)
	}
	return styles.TabActive.Render(title) + "\n" + lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(strings.Repeat("─", max(1, m.width)))
}

func (m *LibraryModel) loadingText() string {
	switch m.selectedSection {
	case sectionSongs:
		return "Loading songs…"
	case sectionAlbums:
		return "Loading albums…"
	case sectionArtists:
		return "Loading artists…"
	case sectionPlaylists:
		return "Loading playlists…"
	default:
		return "Loading…"
	}
}

func (m *LibraryModel) emptyText() string {
	switch m.selectedSection {
	case sectionSongs:
		return "No songs found"
	case sectionAlbums:
		return "No albums found"
	case sectionArtists:
		return "No artists found"
	case sectionPlaylists:
		return "No playlists found"
	default:
		return "No items found"
	}
}

func sectionTitle(section librarySection) string {
	switch section {
	case sectionSongs:
		return "Songs"
	case sectionAlbums:
		return "Albums"
	case sectionArtists:
		return "Artists"
	case sectionPlaylists:
		return "Playlists"
	default:
		panic("unknown library section")
	}
}

func buildLibraryAlbums(tracks []provider.Track) []trackGroup {
	groups := make([]trackGroup, 0)
	index := make(map[string]int, len(tracks))
	for _, track := range tracks {
		title := track.Album
		if title == "" {
			title = "Unknown Album"
		}
		key := track.Artist + "\x00" + title
		if i, ok := index[key]; ok {
			groups[i].tracks = append(groups[i].tracks, track)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, trackGroup{title: title, desc: track.Artist, tracks: []provider.Track{track}})
	}
	return groups
}

func buildLibraryArtists(tracks []provider.Track) []trackGroup {
	groups := make([]trackGroup, 0)
	index := make(map[string]int, len(tracks))
	for _, track := range tracks {
		title := track.Artist
		if title == "" {
			title = "Unknown Artist"
		}
		if i, ok := index[title]; ok {
			groups[i].tracks = append(groups[i].tracks, track)
			continue
		}
		index[title] = len(groups)
		groups = append(groups, trackGroup{title: title, desc: fmt.Sprintf("%d tracks", 1), tracks: []provider.Track{track}})
	}
	for i := range groups {
		groups[i].desc = fmt.Sprintf("%d tracks", len(groups[i].tracks))
	}
	return groups
}

type sectionItem struct{ section librarySection }

func (s sectionItem) Title() string       { return sectionTitle(s.section) }
func (s sectionItem) Description() string { return "enter to browse" }
func (s sectionItem) FilterValue() string { return s.Title() }

type albumItem struct{ group trackGroup }

func (a albumItem) Title() string       { return a.group.title }
func (a albumItem) Description() string { return a.group.desc }
func (a albumItem) FilterValue() string { return a.group.title + " " + a.group.desc }

type artistItem struct{ group trackGroup }

func (a artistItem) Title() string       { return a.group.title }
func (a artistItem) Description() string { return a.group.desc }
func (a artistItem) FilterValue() string { return a.group.title }

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
