package provider

import (
	"context"
	"time"
)

type Track struct {
	ID         string
	CatalogID  string // catalog ID for playback; set for library tracks where ID is "i.XXXXX"
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
	// CreatePlaylist creates a new playlist in the user's library with the given
	// name and the supplied track IDs (library or catalog IDs). It returns the
	// newly created Playlist on success.
	CreatePlaylist(ctx context.Context, name string, trackIDs []string) (Playlist, error)
	// LoveSong adds the song to the user's Apple Music library and marks it as
	// "Loved". catalogID must be the catalog ID (not a library "i." ID).
	// Passing loved=false removes the rating (un-loves the song).
	LoveSong(ctx context.Context, catalogID string, loved bool) error
	// GetSongRating returns true when the given catalog song is marked as Loved
	// in the user's Apple Music account. Returns false (no error) when the song
	// is not rated or the provider does not support ratings.
	GetSongRating(ctx context.Context, catalogID string) (bool, error)
	IsAuthenticated() bool
}
