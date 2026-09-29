// Package local provides a Provider that serves tracks from a local music
// directory. It scans recursively for MP3, FLAC, M4A (plus OGG on Linux and Windows) and reads
// their metadata using the dhowden/tag library. No network or credentials
// are required.

package local

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dhowden/tag"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Provider scans a local directory and exposes tracks via the provider interface.
type Provider struct {
	dir    string
	tracks []provider.Track

	// skipped counts the files the last scan ignored for an unsupported
	// extension, keyed by that extension ("" for a file that has none).
	// unreadable counts files with a supported extension that would not open.
	skipped    map[string]int
	unreadable int
}

// New creates a Provider and performs the initial directory scan.
func New(dir string) (*Provider, error) {
	p := &Provider{dir: dir}
	if err := p.scan(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Provider) Name() string          { return "Local" }
func (p *Provider) IsAuthenticated() bool { return true }

// scan walks dir recursively and indexes all supported audio files.
func (p *Provider) scan() error {
	p.tracks = nil
	p.skipped = map[string]int{}
	p.unreadable = 0
	return filepath.Walk(p.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				return filepath.SkipDir
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			p.skipped[ext]++
			return nil
		}
		t, err := trackFromFile(path)
		if err != nil {
			p.unreadable++
			return nil // skipping files that have unreadable metadata
		}
		p.tracks = append(p.tracks, t)
		return nil
	})
}

// ScanNotice reports what the last scan left out, or "" when every file in the
// directory was indexed. An empty library reaches the TUI looking the same
// whether the directory holds nothing, holds only formats this platform cannot
// play, or holds files that would not open, and only the format case has a
// remedy the user can act on. The supported set is not documented anywhere
// else a user would look, so the message names it.
func (p *Provider) ScanNotice() string {
	skipped := 0
	for _, n := range p.skipped {
		skipped += n
	}
	if skipped == 0 && p.unreadable == 0 && len(p.tracks) > 0 {
		return ""
	}

	dir := shortPath(p.dir)
	head := "no playable tracks in " + dir
	if len(p.tracks) > 0 {
		head = fmt.Sprintf("indexed %d of %d files in %s",
			len(p.tracks), len(p.tracks)+skipped+p.unreadable, dir)
	}

	var parts []string
	if skipped > 0 {
		// The playable set comes before the detail: a status bar truncates
		// from the right, and it is the half the user can act on.
		parts = append(parts,
			platformName+" plays "+supportedExtList(),
			fmt.Sprintf("skipped %s (%s)", fileCount(skipped), skippedExtList(p.skipped)))
	}
	if p.unreadable > 0 {
		parts = append(parts, fileCount(p.unreadable)+" could not be read")
	}
	if len(parts) == 0 {
		return head + ": the directory is empty"
	}
	return head + ": " + strings.Join(parts, "; ")
}

// fileCount renders a count of files with its plural.
func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// skippedExtList names the skipped extensions, commonest first, capped so the
// line stays inside a status bar. A music directory routinely holds artwork,
// cue sheets and logs beside the audio, so the list can be long and the tail
// of it is never the answer.
func skippedExtList(counts map[string]int) string {
	exts := slices.Sorted(maps.Keys(counts))
	slices.SortStableFunc(exts, func(a, b string) int { return cmp.Compare(counts[b], counts[a]) })

	const named = 3
	parts := make([]string, 0, named+1)
	for i, ext := range exts {
		if i == named {
			parts = append(parts, fmt.Sprintf("+%d more", len(exts)-named))
			break
		}
		name := ext
		if name == "" {
			name = "no extension"
		}
		parts = append(parts, fmt.Sprintf("%d %s", counts[ext], name))
	}
	return strings.Join(parts, ", ")
}

// shortPath swaps the home directory for ~. A music directory is usually
// under it, and the path is the one part of the line the user already knows.
func shortPath(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if strings.HasPrefix(dir, home+string(filepath.Separator)) {
		return "~" + dir[len(home):]
	}
	return dir
}

// supportedExtList names this platform's playable extensions.
func supportedExtList() string {
	return strings.Join(slices.Sorted(maps.Keys(supportedExts)), ", ")
}

// trackFromFile reads metadata from an audio file and returns a Track.
func trackFromFile(path string) (provider.Track, error) {
	f, err := os.Open(path) //nolint:gosec // the path comes from user-configured music dir
	if err != nil {
		return provider.Track{}, err
	}
	defer func() { _ = f.Close() }()

	m, err := tag.ReadFrom(f)

	title := filepath.Base(path)
	artist := "Unknown Artist"
	album := "Unknown Album"
	var genres []string

	if err == nil {
		if m.Title() != "" {
			title = m.Title()
		}
		if m.Artist() != "" {
			artist = m.Artist()
		}
		if m.Album() != "" {
			album = m.Album()
		}
		if m.Genre() != "" {
			genres = []string{m.Genre()}
		}
	}

	return provider.Track{
		ID:     fmt.Sprintf("local:%s", path),
		Title:  title,
		Artist: artist,
		Album:  album,
		Genres: genres,
	}, nil
}

func (p *Provider) GetLibraryTracks(_ context.Context) ([]provider.Track, error) {
	out := make([]provider.Track, len(p.tracks))
	copy(out, p.tracks)
	return out, nil
}

func (p *Provider) Search(_ context.Context, query string) (*provider.SearchResult, error) {
	q := strings.ToLower(query)
	var tracks []provider.Track
	var albums []provider.Album
	seen := map[string]bool{}

	for _, t := range p.tracks {
		if strings.Contains(strings.ToLower(t.Title), q) ||
			strings.Contains(strings.ToLower(t.Artist), q) ||
			strings.Contains(strings.ToLower(t.Album), q) {
			tracks = append(tracks, t)
			if !seen[t.Album] {
				seen[t.Album] = true
				albums = append(albums, provider.Album{
					ID:     "local-album:" + t.Album,
					Title:  t.Album,
					Artist: t.Artist,
				})
			}
		}
	}
	return &provider.SearchResult{Tracks: tracks, Albums: albums}, nil
}

func (p *Provider) GetLibraryPlaylists(_ context.Context) ([]provider.Playlist, error) {
	return nil, nil
}

func (p *Provider) GetPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (p *Provider) GetAlbumTracks(_ context.Context, albumID string) ([]provider.Track, error) {
	albumID = strings.TrimPrefix(albumID, "local-album:")
	var out []provider.Track
	for _, t := range p.tracks {
		if t.Album == albumID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (p *Provider) GetLibraryAlbumTracks(ctx context.Context, albumID string) ([]provider.Track, error) {
	return p.GetAlbumTracks(ctx, albumID)
}

func (p *Provider) GetCatalogPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (p *Provider) CreatePlaylist(_ context.Context, _ string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{}, nil
}

func (p *Provider) LoveSong(_ context.Context, _ string, _ bool) error      { return nil }
func (p *Provider) GetSongRating(_ context.Context, _ string) (bool, error) { return false, nil }
func (p *Provider) AddToPlaylist(_ context.Context, _, _ string) error      { return nil }

func (p *Provider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	// Group tracks by genre using their metadata tags.
	// Tracks with no genre tag are skipped.
	byGenre := map[string][]provider.Track{}
	for _, t := range p.tracks {
		for _, g := range t.Genres {
			if g != "" {
				byGenre[g] = append(byGenre[g], t)
			}
		}
	}

	var groups []provider.RecommendationGroup
	for genre, tracks := range byGenre {
		if len(tracks) < 2 {
			continue
		}
		items := make([]provider.RecommendationItem, 0, len(tracks))
		for _, t := range tracks {
			items = append(items, provider.RecommendationItem{
				ID:       t.ID,
				Kind:     "track",
				Title:    t.Title,
				Subtitle: t.Artist,
			})
		}
		groups = append(groups, provider.RecommendationGroup{
			Title: genre,
			Items: items,
		})
	}
	return groups, nil
}

func (p *Provider) GetStationTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
