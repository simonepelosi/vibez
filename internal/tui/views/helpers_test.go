package views

import (
	"context"

	"github.com/simone-vibes/vibez/internal/provider"
)

// mockProvider is a no-op provider for unit tests.
type mockProvider struct {
	searchResult  *provider.SearchResult
	searchErr     error
	libraryTracks []provider.Track
	playlists     []provider.Playlist
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

func (m *mockProvider) GetLibraryTracks(_ context.Context) ([]provider.Track, error) {
	return m.libraryTracks, nil
}

func (m *mockProvider) GetLibraryPlaylists(_ context.Context) ([]provider.Playlist, error) {
	return m.playlists, nil
}

func (m *mockProvider) GetPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (m *mockProvider) GetAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (m *mockProvider) CreatePlaylist(_ context.Context, _ string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{}, nil
}
func (m *mockProvider) LoveSong(_ context.Context, _ string, _ bool) error      { return nil }
func (m *mockProvider) GetSongRating(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mockProvider) IsAuthenticated() bool                                   { return true }
func (m *mockProvider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	return nil, nil
}
