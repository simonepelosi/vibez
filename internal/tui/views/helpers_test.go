package views

import (
	"context"

	"github.com/simone-vibes/vibez/internal/provider"
)

// mockProvider is a no-op provider for unit tests.
type mockProvider struct {
	searchResult       *provider.SearchResult
	searchErr          error
	libraryTracks      []provider.Track
	playlists          []provider.Playlist
	playlistTracks     map[string][]provider.Track
	libraryTrackCalls  int
	libraryTrackCtx    context.Context
	playlistCalls      int
	playlistCtx        context.Context
	playlistTrackCalls int
	playlistTrackCtx   context.Context
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Search(_ context.Context, _ string) (*provider.SearchResult, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	if m.searchResult != nil {
		return m.searchResult, nil
	}
	return &provider.SearchResult{}, nil
}

func (m *mockProvider) GetLibraryTracks(ctx context.Context) ([]provider.Track, error) {
	m.libraryTrackCalls++
	m.libraryTrackCtx = ctx
	return m.libraryTracks, nil
}

func (m *mockProvider) GetLibraryPlaylists(ctx context.Context) ([]provider.Playlist, error) {
	m.playlistCalls++
	m.playlistCtx = ctx
	return m.playlists, nil
}

func (m *mockProvider) GetPlaylistTracks(ctx context.Context, id string) ([]provider.Track, error) {
	m.playlistTrackCalls++
	m.playlistTrackCtx = ctx
	if m.playlistTracks == nil {
		return nil, nil
	}
	return m.playlistTracks[id], nil
}

func (m *mockProvider) GetAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (m *mockProvider) GetLibraryAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (m *mockProvider) GetCatalogPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (m *mockProvider) CreatePlaylist(_ context.Context, _ string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{}, nil
}
func (m *mockProvider) LoveSong(_ context.Context, _ string, _ bool) error      { return nil }
func (m *mockProvider) GetSongRating(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mockProvider) IsAuthenticated() bool                                   { return true }
func (m *mockProvider) AddToPlaylist(_ context.Context, _, _ string) error      { return nil }
func (m *mockProvider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	return nil, nil
}
func (m *mockProvider) GetStationTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
