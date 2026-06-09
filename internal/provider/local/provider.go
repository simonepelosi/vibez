// Package local provides a Provider that serves tracks from a local music
// directory. It scans recursively for MP3, FLAC, M4A and OGG files and reads
// their metadata using the dhowden/tag library. No network or credentials
// are required.

package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"github.com/simone-vibes/vibez/internal/provider"
)

var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".ogg":  true,
}

// Provider scans a local directory and exposes tracks via the provider interface.
type Provider struct {
	dir    string
	tracks []provider.Track
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
	return filepath.Walk(p.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			return nil
		}
		t, err := trackFromFile(path)
		if err != nil {
			return nil // skipping files that have unreadable metadata
		}
		p.tracks = append(p.tracks, t)
		return nil
	})
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
