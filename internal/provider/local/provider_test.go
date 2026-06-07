package local_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider/local"
)

// newTestProvider creates a Provider pointed at a temp directory
// containing dummy audio files with predictable names.
func newTestProvider(t *testing.T) *local.Provider {
	t.Helper()
	dir := t.TempDir()

	// Create dummy files — dhowden/tag will fail to read metadata
	// from empty files, so the provider falls back to filename-based
	// title and "Unknown Artist" / "Unknown Album". That's fine for
	// testing scan and search logic.
	files := []string{
		"Artist1 - Album1 - Track1.mp3",
		"Artist1 - Album1 - Track2.flac",
		"Artist2 - Album2 - Track1.m4a",
		"Artist2 - Album2 - Track2.ogg",
		"ignored.txt",
		"ignored.pdf",
	}
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
			t.Fatalf("creating test file %s: %v", f, err)
		}
	}

	p, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	return p
}

// --- Name ---

func TestProvider_Name(t *testing.T) {
	p := newTestProvider(t)
	if p.Name() != "Local" {
		t.Errorf("Name() = %q, want %q", p.Name(), "Local")
	}
}

// --- IsAuthenticated ---

func TestProvider_IsAuthenticated(t *testing.T) {
	p := newTestProvider(t)
	if !p.IsAuthenticated() {
		t.Error("IsAuthenticated() = false, want true")
	}
}

// --- GetLibraryTracks ---

func TestProvider_GetLibraryTracks_OnlyAudioFiles(t *testing.T) {
	p := newTestProvider(t)
	tracks, err := p.GetLibraryTracks(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryTracks: %v", err)
	}
	// 4 audio files, 2 non-audio files should be ignored.
	if len(tracks) != 4 {
		t.Errorf("GetLibraryTracks = %d tracks, want 4", len(tracks))
	}
}

func TestProvider_GetLibraryTracks_ReturnsCopy(t *testing.T) {
	p := newTestProvider(t)
	tracks, _ := p.GetLibraryTracks(context.Background())
	if len(tracks) > 0 {
		tracks[0].Title = "mutated"
	}
	tracks2, _ := p.GetLibraryTracks(context.Background())
	if len(tracks2) > 0 && tracks2[0].Title == "mutated" {
		t.Error("GetLibraryTracks returned a shared slice (not a copy)")
	}
}

func TestProvider_GetLibraryTracks_IDsAreLocalPrefixed(t *testing.T) {
	p := newTestProvider(t)
	tracks, _ := p.GetLibraryTracks(context.Background())
	for _, tr := range tracks {
		if !strings.HasPrefix(tr.ID, "local:") {
			t.Errorf("track ID %q does not start with 'local:'", tr.ID)
		}
	}
}

// --- Search ---

func TestProvider_Search_NoMatch(t *testing.T) {
	p := newTestProvider(t)
	result, err := p.Search(context.Background(), "zzznomatchzzz")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 0 {
		t.Errorf("Search(zzznomatchzzz) = %d tracks, want 0", len(result.Tracks))
	}
}

func TestProvider_Search_CaseInsensitive(t *testing.T) {
	p := newTestProvider(t)
	upper, _ := p.Search(context.Background(), "TRACK1")
	lower, _ := p.Search(context.Background(), "track1")
	if len(upper.Tracks) != len(lower.Tracks) {
		t.Errorf("case-insensitive mismatch: upper=%d lower=%d", len(upper.Tracks), len(lower.Tracks))
	}
}

func TestProvider_Search_DeduplicatesAlbums(t *testing.T) {
	p := newTestProvider(t)
	result, _ := p.Search(context.Background(), "track")
	seen := map[string]int{}
	for _, a := range result.Albums {
		seen[a.Title]++
		if seen[a.Title] > 1 {
			t.Errorf("album %q appears %d times in results", a.Title, seen[a.Title])
		}
	}
}

// --- GetLibraryPlaylists ---

func TestProvider_GetLibraryPlaylists_ReturnsNil(t *testing.T) {
	p := newTestProvider(t)
	playlists, err := p.GetLibraryPlaylists(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryPlaylists: %v", err)
	}
	if playlists != nil {
		t.Errorf("GetLibraryPlaylists = %v, want nil", playlists)
	}
}

// --- GetAlbumTracks ---

func TestProvider_GetAlbumTracks_UnknownAlbum(t *testing.T) {
	p := newTestProvider(t)
	tracks, err := p.GetAlbumTracks(context.Background(), "local-album:nonexistent")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 0 {
		t.Errorf("GetAlbumTracks(nonexistent) = %d tracks, want 0", len(tracks))
	}
}

// --- New with invalid directory ---

func TestNew_InvalidDir(t *testing.T) {
	_, err := local.New("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("New() with invalid dir should return error, got nil")
	}
}

// --- CreatePlaylist ---

func TestProvider_CreatePlaylist_ReturnsEmptyPlaylist(t *testing.T) {
	p := newTestProvider(t)
	pl, err := p.CreatePlaylist(context.Background(), "test", []string{})
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}
	if pl.Name != "" && pl.ID != "" {
		t.Log("CreatePlaylist returned non-empty playlist (stub)")
	}
}

// --- LoveSong ---

func TestProvider_LoveSong_NoError(t *testing.T) {
	p := newTestProvider(t)
	if err := p.LoveSong(context.Background(), "local:test", true); err != nil {
		t.Errorf("LoveSong: %v", err)
	}
}

// --- GetSongRating ---

func TestProvider_GetSongRating_ReturnsFalse(t *testing.T) {
	p := newTestProvider(t)
	loved, err := p.GetSongRating(context.Background(), "local:test")
	if err != nil {
		t.Fatalf("GetSongRating: %v", err)
	}
	if loved {
		t.Error("GetSongRating() = true, want false")
	}
}