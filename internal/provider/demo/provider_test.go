package demo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider/demo"
)

func newProvider() demo.Provider {
	return demo.Provider{}
}

// --- Name ---

func TestProvider_Name(t *testing.T) {
	p := newProvider()
	if p.Name() != "Demo" {
		t.Errorf("Name() = %q, want %q", p.Name(), "Demo")
	}
}

// --- IsAuthenticated ---

func TestProvider_IsAuthenticated(t *testing.T) {
	p := newProvider()
	if !p.IsAuthenticated() {
		t.Error("IsAuthenticated() = false, want true")
	}
}

// --- GetLibraryTracks ---

func TestProvider_GetLibraryTracks_ReturnsAllTracks(t *testing.T) {
	p := newProvider()
	tracks, err := p.GetLibraryTracks(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryTracks: %v", err)
	}
	if len(tracks) != len(demo.Tracks) {
		t.Errorf("GetLibraryTracks = %d tracks, want %d", len(tracks), len(demo.Tracks))
	}
}

func TestProvider_GetLibraryTracks_ReturnsCopy(t *testing.T) {
	p := newProvider()
	tracks, _ := p.GetLibraryTracks(context.Background())
	// Mutating the returned slice must not affect future calls.
	if len(tracks) > 0 {
		tracks[0].Title = "mutated"
	}
	tracks2, _ := p.GetLibraryTracks(context.Background())
	if len(tracks2) > 0 && tracks2[0].Title == "mutated" {
		t.Error("GetLibraryTracks returned a shared slice (not a copy)")
	}
}

// --- GetLibraryPlaylists ---

func TestProvider_GetLibraryPlaylists_NotEmpty(t *testing.T) {
	p := newProvider()
	playlists, err := p.GetLibraryPlaylists(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryPlaylists: %v", err)
	}
	if len(playlists) == 0 {
		t.Error("GetLibraryPlaylists returned empty list")
	}
}

func TestProvider_GetLibraryPlaylists_ReturnsCopy(t *testing.T) {
	p := newProvider()
	pls, _ := p.GetLibraryPlaylists(context.Background())
	if len(pls) > 0 {
		pls[0].Name = "mutated"
	}
	pls2, _ := p.GetLibraryPlaylists(context.Background())
	if len(pls2) > 0 && pls2[0].Name == "mutated" {
		t.Error("GetLibraryPlaylists returned a shared slice (not a copy)")
	}
}

// --- GetPlaylistTracks ---

func TestProvider_GetPlaylistTracks_KnownPlaylist(t *testing.T) {
	p := newProvider()
	tracks, err := p.GetPlaylistTracks(context.Background(), "dp1")
	if err != nil {
		t.Fatalf("GetPlaylistTracks(dp1): %v", err)
	}
	if len(tracks) == 0 {
		t.Error("GetPlaylistTracks(dp1) returned empty slice")
	}
}

func TestProvider_GetPlaylistTracks_AllPlaylists(t *testing.T) {
	p := newProvider()
	ids := []string{"dp1", "dp2", "dp3"}
	for _, id := range ids {
		tracks, err := p.GetPlaylistTracks(context.Background(), id)
		if err != nil {
			t.Errorf("GetPlaylistTracks(%s): %v", id, err)
		}
		if len(tracks) == 0 {
			t.Errorf("GetPlaylistTracks(%s) returned empty slice", id)
		}
	}
}

func TestProvider_GetPlaylistTracks_UnknownPlaylist(t *testing.T) {
	p := newProvider()
	tracks, err := p.GetPlaylistTracks(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetPlaylistTracks(nonexistent): %v", err)
	}
	if tracks != nil {
		t.Errorf("GetPlaylistTracks(nonexistent) = %v, want nil", tracks)
	}
}

func TestProvider_GetPlaylistTracks_ReturnsCopy(t *testing.T) {
	p := newProvider()
	tracks, _ := p.GetPlaylistTracks(context.Background(), "dp1")
	if len(tracks) > 0 {
		tracks[0].Title = "mutated"
	}
	tracks2, _ := p.GetPlaylistTracks(context.Background(), "dp1")
	if len(tracks2) > 0 && tracks2[0].Title == "mutated" {
		t.Error("GetPlaylistTracks returned a shared slice (not a copy)")
	}
}

// --- GetAlbumTracks ---

func TestProvider_GetAlbumTracks_ReturnsAllTracks(t *testing.T) {
	p := newProvider()
	tracks, err := p.GetAlbumTracks(context.Background(), "any-id")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) == 0 {
		t.Error("GetAlbumTracks returned empty slice")
	}
}

// --- Search ---

func TestProvider_Search_MatchesTitle(t *testing.T) {
	p := newProvider()
	result, err := p.Search(context.Background(), "Nights")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) == 0 {
		t.Error("Search(Nights) returned no tracks")
	}
	found := false
	for _, tr := range result.Tracks {
		if strings.EqualFold(tr.Title, "Nights") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Search(Nights) did not return the 'Nights' track")
	}
}

func TestProvider_Search_MatchesArtist(t *testing.T) {
	p := newProvider()
	result, err := p.Search(context.Background(), "Frank Ocean")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) == 0 {
		t.Error("Search(Frank Ocean) returned no tracks")
	}
	for _, tr := range result.Tracks {
		if !strings.EqualFold(tr.Artist, "Frank Ocean") {
			t.Errorf("unexpected track artist %q in Frank Ocean search", tr.Artist)
		}
	}
}

func TestProvider_Search_MatchesAlbum(t *testing.T) {
	p := newProvider()
	result, err := p.Search(context.Background(), "Blonde")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) == 0 {
		t.Error("Search(Blonde) returned no tracks")
	}
}

func TestProvider_Search_CaseInsensitive(t *testing.T) {
	p := newProvider()
	upper, _ := p.Search(context.Background(), "NIGHTS")
	lower, _ := p.Search(context.Background(), "nights")
	if len(upper.Tracks) != len(lower.Tracks) {
		t.Errorf("case-insensitive search mismatch: upper=%d lower=%d tracks", len(upper.Tracks), len(lower.Tracks))
	}
}

func TestProvider_Search_NoMatch(t *testing.T) {
	p := newProvider()
	result, err := p.Search(context.Background(), "zzznomatchzzz")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 0 {
		t.Errorf("Search(zzznomatchzzz) = %d tracks, want 0", len(result.Tracks))
	}
}

func TestProvider_Search_ReturnsAlbums(t *testing.T) {
	p := newProvider()
	result, err := p.Search(context.Background(), "Frank Ocean")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Albums) == 0 {
		t.Error("Search(Frank Ocean) returned no albums")
	}
}

func TestProvider_Search_DeduplicatesAlbums(t *testing.T) {
	p := newProvider()
	result, _ := p.Search(context.Background(), "Frank Ocean")
	seen := map[string]int{}
	for _, a := range result.Albums {
		seen[a.Title]++
		if seen[a.Title] > 1 {
			t.Errorf("album %q appears %d times in search results", a.Title, seen[a.Title])
		}
	}
}

// --- CreatePlaylist ---

func TestProvider_CreatePlaylist_ReturnsPlaylist(t *testing.T) {
	p := newProvider()
	pl, err := p.CreatePlaylist(context.Background(), "My New Playlist", []string{"d1", "d2"})
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}
	if pl.Name != "My New Playlist" {
		t.Errorf("Name = %q, want %q", pl.Name, "My New Playlist")
	}
	if pl.ID == "" {
		t.Error("CreatePlaylist returned empty ID")
	}
}

// --- LoveSong ---

func TestProvider_LoveSong_NoError(t *testing.T) {
	p := newProvider()
	if err := p.LoveSong(context.Background(), "d1", true); err != nil {
		t.Errorf("LoveSong: %v", err)
	}
	if err := p.LoveSong(context.Background(), "d1", false); err != nil {
		t.Errorf("LoveSong(unlike): %v", err)
	}
}

// --- GetSongRating ---

func TestProvider_GetSongRating_ReturnsFalse(t *testing.T) {
	p := newProvider()
	loved, err := p.GetSongRating(context.Background(), "d1")
	if err != nil {
		t.Fatalf("GetSongRating: %v", err)
	}
	if loved {
		t.Error("GetSongRating() = true, want false (demo always returns false)")
	}
}
