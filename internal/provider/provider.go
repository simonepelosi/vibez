package provider

import (
	"context"
	"time"
)

type Track struct {
	ID         string
	Title      string
	Artist     string
	Album      string
	Duration   time.Duration
	ArtworkURL string
	PreviewURL string
	Genres     []string
}

type Album struct {
	ID         string
	Title      string
	Artist     string
	ArtworkURL string
	TrackCount int
}

type Playlist struct {
	ID         string
	Name       string
	TrackCount int
	ArtworkURL string
}

type SearchResult struct {
	Tracks    []Track
	Albums    []Album
	Playlists []Playlist
}

type Provider interface {
	Name() string
	Search(ctx context.Context, query string) (*SearchResult, error)
	GetLibraryTracks(ctx context.Context) ([]Track, error)
	GetLibraryPlaylists(ctx context.Context) ([]Playlist, error)
	GetPlaylistTracks(ctx context.Context, playlistID string) ([]Track, error)
	GetAlbumTracks(ctx context.Context, albumID string) ([]Track, error)
	IsAuthenticated() bool
}
