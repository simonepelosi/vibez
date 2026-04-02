package apple_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/provider/apple"
)

// newTestProvider creates an AppleProvider pointed at a test server.
func newTestProvider(t *testing.T, srv *httptest.Server) *apple.AppleProvider {
	t.Helper()
	cfg := &config.Config{ //nolint:gosec // G101: test credentials, not real secrets
		AppleDeveloperToken: "test-dev-token",
		AppleUserToken:      "test-user-token",
		StoreFront:          "us",
	}
	p := apple.New(cfg)
	p.SetBaseURL(srv.URL) // injected for tests
	return p
}

func newAuthedConfig() *config.Config {
	return &config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
		AuthPort:            7777,
	}
}

// --- IsAuthenticated ---

func TestIsAuthenticated_BothTokensPresent(t *testing.T) {
	p := apple.New(newAuthedConfig())
	if !p.IsAuthenticated() {
		t.Error("expected authenticated, got false")
	}
}

func TestIsAuthenticated_MissingDevToken(t *testing.T) {
	cfg := newAuthedConfig()
	cfg.AppleDeveloperToken = ""
	p := apple.New(cfg)
	if p.IsAuthenticated() {
		t.Error("expected not authenticated (missing dev token)")
	}
}

func TestIsAuthenticated_MissingUserToken(t *testing.T) {
	cfg := newAuthedConfig()
	cfg.AppleUserToken = ""
	p := apple.New(cfg)
	if p.IsAuthenticated() {
		t.Error("expected not authenticated (missing user token)")
	}
}

func TestName(t *testing.T) {
	p := apple.New(newAuthedConfig())
	if p.Name() != "apple" {
		t.Errorf("Name() = %q, want %q", p.Name(), "apple")
	}
}

// --- Search ---

// searchHandler routes the two Search sub-requests by path:
//
//	/me/library/search   → libraryResp
//	/catalog/{sf}/search → catalogResp  (albums/playlists only; no songs)
func searchHandler(t *testing.T, w http.ResponseWriter, r *http.Request, libraryResp, catalogResp any) {
	t.Helper()
	if strings.Contains(r.URL.Path, "/me/library/search") {
		writeJSON(t, w, libraryResp)
	} else {
		writeJSON(t, w, catalogResp)
	}
}

func TestSearch_ReturnsTracksAlbumsPlaylists(t *testing.T) {
	// Library song comes first; catalog adds album and playlist.
	libSong := songJSON("i.Abc123", "Humble", "Kendrick Lamar", "DAMN.", 212000, "https://art/{w}x{h}.jpg")
	libResp := map[string]any{
		"results": map[string]any{
			"library-songs":     map[string]any{"data": []any{libSong}},
			"library-albums":    map[string]any{"data": []any{}},
			"library-playlists": map[string]any{"data": []any{}},
		},
	}
	catResp := map[string]any{
		"results": map[string]any{
			"songs":     map[string]any{"data": []any{}},
			"albums":    map[string]any{"data": []any{albumJSON("a1", "DAMN.", "Kendrick Lamar", 14)}},
			"playlists": map[string]any{"data": []any{playlistJSON("p1", "Hip Hop Hits", 50)}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-dev-token" {
			t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Music-User-Token") != "test-user-token" {
			t.Errorf("Music-User-Token header = %q", r.Header.Get("Music-User-Token"))
		}
		searchHandler(t, w, r, libResp, catResp)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "kendrick")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(result.Tracks) != 1 {
		t.Fatalf("Tracks: got %d, want 1", len(result.Tracks))
	}
	tk := result.Tracks[0]
	if tk.ID != "i.Abc123" {
		t.Errorf("Track.ID = %q, want i.Abc123", tk.ID)
	}
	if tk.Title != "Humble" {
		t.Errorf("Track.Title = %q", tk.Title)
	}
	if tk.Artist != "Kendrick Lamar" {
		t.Errorf("Track.Artist = %q", tk.Artist)
	}
	if tk.Duration != 212*time.Second {
		t.Errorf("Track.Duration = %v, want 212s", tk.Duration)
	}
	if tk.ArtworkURL != "https://art/300x300.jpg" {
		t.Errorf("Track.ArtworkURL = %q", tk.ArtworkURL)
	}
	if len(result.Albums) != 1 || result.Albums[0].ID != "a1" {
		t.Errorf("Albums mismatch: %+v", result.Albums)
	}
	if len(result.Playlists) != 1 || result.Playlists[0].ID != "p1" {
		t.Errorf("Playlists mismatch: %+v", result.Playlists)
	}
}

func TestSearch_CatalogTracksIncluded(t *testing.T) {
	// Catalog tracks appear when they are not already in the library.
	catSong := songJSON("999", "New Song", "New Artist", "New Album", 180000, "")
	libEmpty := map[string]any{"results": map[string]any{}}
	catResp := map[string]any{
		"results": map[string]any{
			"songs":     map[string]any{"data": []any{catSong}},
			"albums":    map[string]any{"data": []any{}},
			"playlists": map[string]any{"data": []any{}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHandler(t, w, r, libEmpty, catResp)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "new song")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 1 || result.Tracks[0].ID != "999" {
		t.Errorf("expected catalog track 999, got %+v", result.Tracks)
	}
}

func TestSearch_UnplayableCatalogTracksDropped(t *testing.T) {
	// Songs without playParams are radio-only and must be filtered out.
	playable := songJSON("111", "Playable Song", "Artist A", "Album A", 200000, "")
	unplayable := songJSONNoPlay("222", "Radio Song", "Artist B", "Album B", 200000)
	libEmpty := map[string]any{"results": map[string]any{}}
	catResp := map[string]any{
		"results": map[string]any{
			"songs":     map[string]any{"data": []any{playable, unplayable}},
			"albums":    map[string]any{"data": []any{}},
			"playlists": map[string]any{"data": []any{}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHandler(t, w, r, libEmpty, catResp)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "song")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 1 || result.Tracks[0].ID != "111" {
		t.Errorf("expected only playable track 111, got %+v", result.Tracks)
	}
}

func TestSearch_LibraryTracksOnly(t *testing.T) {
	// Library song deduplicates the same song from catalog (library wins).
	libSong := songJSON("i.AbCdEf", "Humble", "Kendrick Lamar", "DAMN.", 212000, "")
	catSong := songJSON("1234", "Humble", "Kendrick Lamar", "DAMN.", 212000, "")

	libResp := map[string]any{
		"results": map[string]any{
			"library-songs":     map[string]any{"data": []any{libSong}},
			"library-albums":    map[string]any{"data": []any{}},
			"library-playlists": map[string]any{"data": []any{}},
		},
	}
	catResp := map[string]any{
		"results": map[string]any{
			"songs":     map[string]any{"data": []any{catSong}},
			"albums":    map[string]any{"data": []any{}},
			"playlists": map[string]any{"data": []any{}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHandler(t, w, r, libResp, catResp)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "humble")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 1 {
		t.Fatalf("Tracks: got %d, want 1 (deduped)", len(result.Tracks))
	}
	if result.Tracks[0].ID != "i.AbCdEf" {
		t.Errorf("expected library ID i.AbCdEf, got %q", result.Tracks[0].ID)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	empty := map[string]any{"results": map[string]any{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHandler(t, w, r, empty, empty)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "zzznoresults")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 0 || len(result.Albums) != 0 || len(result.Playlists) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestSearch_QueryEncoded(t *testing.T) {
	var gotURLs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURLs = append(gotURLs, r.URL.RawQuery)
		writeJSON(t, w, map[string]any{"results": map[string]any{}})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	_, _ = p.Search(context.Background(), "lofi hip hop")

	found := false
	for _, q := range gotURLs {
		if containsStr(q, "lofi+hip+hop") || containsStr(q, "lofi%20hip%20hop") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("query not encoded correctly in any request: %v", gotURLs)
	}
}

func TestSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	_, err := p.Search(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
}

func TestSearch_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writeJSON(t, w, map[string]any{"results": map[string]any{}})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := newTestProvider(t, srv)
	_, err := p.Search(ctx, "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// --- GetLibraryTracks ---

func TestGetLibraryTracks_SinglePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": []any{
				songJSON("10", "Song A", "Artist A", "Album A", 180000, ""),
				songJSON("11", "Song B", "Artist B", "Album B", 200000, ""),
			},
			"next": "",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	tracks, err := p.GetLibraryTracks(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}
	if tracks[0].Title != "Song A" {
		t.Errorf("tracks[0].Title = %q", tracks[0].Title)
	}
}

func TestGetLibraryTracks_Pagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			writeJSON(t, w, map[string]any{
				"data": []any{songJSON("1", "Track 1", "Art", "Alb", 100000, "")},
				"next": "/me/library/songs?limit=100&offset=100",
			})
		case 2:
			writeJSON(t, w, map[string]any{
				"data": []any{songJSON("2", "Track 2", "Art", "Alb", 100000, "")},
				"next": "",
			})
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	tracks, err := p.GetLibraryTracks(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2 (pagination)", len(tracks))
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls for pagination, got %d", calls)
	}
}

// --- GetLibraryPlaylists ---

func TestGetLibraryPlaylists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": []any{
				playlistJSON("pl1", "Favorites", 10),
				playlistJSON("pl2", "Workout", 25),
			},
			"next": "",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	lists, err := p.GetLibraryPlaylists(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryPlaylists: %v", err)
	}
	if len(lists) != 2 {
		t.Fatalf("got %d playlists, want 2", len(lists))
	}
	if lists[0].Name != "Favorites" {
		t.Errorf("Name = %q", lists[0].Name)
	}
	if lists[1].TrackCount != 25 {
		t.Errorf("TrackCount = %d", lists[1].TrackCount)
	}
}

// --- GetPlaylistTracks ---

func TestGetPlaylistTracks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !containsStr(r.URL.Path, "pl-abc") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"data": []any{
				songJSON("s1", "Playlist Track", "Some Artist", "Some Album", 240000, ""),
			},
			"next": "",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	tracks, err := p.GetPlaylistTracks(context.Background(), "pl-abc")
	if err != nil {
		t.Fatalf("GetPlaylistTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}
	if tracks[0].Title != "Playlist Track" {
		t.Errorf("Title = %q", tracks[0].Title)
	}
}

// --- GetAlbumTracks ---

func TestGetAlbumTracks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": []any{
				songJSON("a1", "Album Track 1", "Artist", "Album", 200000, ""),
				songJSON("a2", "Album Track 2", "Artist", "Album", 220000, ""),
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	tracks, err := p.GetAlbumTracks(context.Background(), "alb-xyz")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}
}

// --- Track preview URL ---

func TestSearch_TrackPreviewURL(t *testing.T) {
	// PreviewURL must be preserved for library songs.
	libSong := map[string]any{
		"id": "i.preview99",
		"attributes": map[string]any{
			"name":             "Test Song",
			"artistName":       "Test Artist",
			"albumName":        "Test Album",
			"durationInMillis": 180000,
			"artwork":          map[string]any{"url": "", "width": 300, "height": 300},
			"previews": []any{
				map[string]any{"url": "https://preview.example.com/song.m4a"},
			},
			"genreNames": []string{"pop"},
		},
	}
	libResp := map[string]any{
		"results": map[string]any{
			"library-songs":     map[string]any{"data": []any{libSong}},
			"library-albums":    map[string]any{"data": []any{}},
			"library-playlists": map[string]any{"data": []any{}},
		},
	}
	catEmpty := map[string]any{"results": map[string]any{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchHandler(t, w, r, libResp, catEmpty)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) == 0 {
		t.Fatal("no tracks returned")
	}
	if result.Tracks[0].PreviewURL != "https://preview.example.com/song.m4a" {
		t.Errorf("PreviewURL = %q", result.Tracks[0].PreviewURL)
	}
}

// --- helpers ---

func songJSON(id, name, artist, album string, durationMs int, artURL string) map[string]any {
	return map[string]any{
		"id": id,
		"attributes": map[string]any{
			"name":             name,
			"artistName":       artist,
			"albumName":        album,
			"durationInMillis": durationMs,
			"artwork":          map[string]any{"url": artURL, "width": 300, "height": 300},
			"previews":         []any{},
			"genreNames":       []string{},
			// Catalog songs with playParams are definitively playable.
			"playParams": map[string]any{"id": id, "kind": "song"},
		},
	}
}

// songJSONNoPlay produces a catalog song fixture without playParams (radio-only / unplayable).
func songJSONNoPlay(id, name, artist, album string, durationMs int) map[string]any {
	return map[string]any{
		"id": id,
		"attributes": map[string]any{
			"name":             name,
			"artistName":       artist,
			"albumName":        album,
			"durationInMillis": durationMs,
			"artwork":          map[string]any{"url": "", "width": 300, "height": 300},
			"previews":         []any{},
			"genreNames":       []string{},
			// no playParams — should be filtered out by Search()
		},
	}
}

func albumJSON(id, name, artist string, trackCount int) map[string]any {
	return map[string]any{
		"id": id,
		"attributes": map[string]any{
			"name":       name,
			"artistName": artist,
			"trackCount": trackCount,
			"artwork":    map[string]any{"url": "", "width": 300, "height": 300},
		},
	}
}

func playlistJSON(id, name string, trackCount int) map[string]any {
	return map[string]any{
		"id": id,
		"attributes": map[string]any{
			"name":       name,
			"trackCount": trackCount,
			"artwork":    map[string]any{"url": "", "width": 300, "height": 300},
		},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("writeJSON: %v", err)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// --- Error path tests ---

func TestGetLibraryTracks_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	_, err := p.GetLibraryTracks(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestGetLibraryPlaylists_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	_, err := p.GetLibraryPlaylists(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
}

func TestGetPlaylistTracks_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	_, err := p.GetPlaylistTracks(context.Background(), "pl-id")
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

func TestGetAlbumTracks_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	_, err := p.GetAlbumTracks(context.Background(), "alb-id")
	if err == nil {
		t.Fatal("expected error for HTTP 503, got nil")
	}
}

func TestSearch_NoResultsKey(t *testing.T) {
	// Valid JSON but no recognized keys — both endpoints return empty objects.
	empty := map[string]any{"results": map[string]any{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, empty)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	result, err := p.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Tracks) != 0 || len(result.Albums) != 0 || len(result.Playlists) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}
