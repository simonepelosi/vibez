package apple_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func songJSONWithVariants(id string, variants ...string) map[string]any {
	s := songJSON(id, "Song "+id, "Artist", "Album", 1000, "")
	s["attributes"].(map[string]any)["audioVariants"] = variants
	return s
}

// The catalog song calls ask for extend=audioVariants and carry the result
// onto the Track; library calls must not ask, since library resources have no
// audio variants.
func TestCatalogSongCallsRequestAudioVariants(t *testing.T) {
	var (
		mu     sync.Mutex
		extend = map[string]string{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if q := r.URL.Query().Get("types"); q != "" {
			key += "?types=" + q
		}
		mu.Lock()
		extend[key] = r.URL.Query().Get("extend")
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/search"):
			writeJSON(t, w, map[string]any{"results": map[string]any{
				"songs": map[string]any{"data": []any{songJSONWithVariants("s1", "lossless")}},
			}})
		default:
			writeJSON(t, w, map[string]any{"data": []any{songJSONWithVariants("1", "lossy-stereo", "lossless", "hi-res-lossless", "dolby-atmos")}})
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	ctx := context.Background()

	album, err := p.GetAlbumTracks(ctx, "a1")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	want := []string{"lossy-stereo", "lossless", "hi-res-lossless", "dolby-atmos"}
	if len(album) != 1 || !slices.Equal(album[0].AudioVariants, want) {
		t.Errorf("GetAlbumTracks AudioVariants = %+v, want %v", album, want)
	}
	if _, err := p.GetCatalogPlaylistTracks(ctx, "pl.1"); err != nil {
		t.Fatalf("GetCatalogPlaylistTracks: %v", err)
	}
	if _, err := p.GetCatalogTracks(ctx, []string{"1"}); err != nil {
		t.Fatalf("GetCatalogTracks: %v", err)
	}
	if _, err := p.GetLibraryAlbumTracks(ctx, "l.1"); err != nil {
		t.Fatalf("GetLibraryAlbumTracks: %v", err)
	}
	res, err := p.Search(ctx, "x")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Tracks) != 1 || !slices.Equal(res.Tracks[0].AudioVariants, []string{"lossless"}) {
		t.Errorf("Search tracks = %+v, want one lossless track", res.Tracks)
	}

	mu.Lock()
	defer mu.Unlock()
	for path, want := range map[string]string{
		"/catalog/us/albums/a1/tracks":                             "audioVariants",
		"/catalog/us/playlists/pl.1/tracks":                        "audioVariants",
		"/catalog/us/songs":                                        "audioVariants",
		"/catalog/us/search?types=songs":                           "audioVariants",
		"/catalog/us/search?types=albums,artists,playlists":        "",
		"/me/library/albums/l.1/tracks":                            "",
		"/me/library/search?types=library-songs,library-playlists": "",
	} {
		got, ok := extend[path]
		if !ok {
			t.Errorf("no request to %s; saw %v", path, extend)
			continue
		}
		if got != want {
			t.Errorf("%s extend = %q, want %q", path, got, want)
		}
	}
}

// An endpoint that refuses the extension must cost the badge, not the call.
func TestAudioVariantsFallsBackWhenRejected(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("extend") != "" {
			http.Error(w, `{"errors":[{"code":"40005"}]}`, http.StatusBadRequest)
			return
		}
		writeJSON(t, w, map[string]any{"data": []any{songJSON("1", "Song", "Artist", "Album", 1000, "")}})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	tracks, err := p.GetAlbumTracks(context.Background(), "a1")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 1 || tracks[0].ID != "1" {
		t.Errorf("tracks = %+v, want song 1", tracks)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (extended, then plain)", got)
	}
}

// Any other failure is not retried.
func TestAudioVariantsDoesNotRetryOtherErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	if _, err := p.GetCatalogTracks(context.Background(), []string{"1"}); err == nil {
		t.Fatal("GetCatalogTracks: want an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// Apple's next links may drop the extend parameter; later pages still ask.
func TestAudioVariantsFollowsNextLinks(t *testing.T) {
	var (
		mu      sync.Mutex
		extends []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		extends = append(extends, r.URL.Query().Get("extend"))
		mu.Unlock()
		if r.URL.Query().Get("offset") == "" {
			writeJSON(t, w, map[string]any{
				"data": []any{songJSONWithVariants("1", "lossless")},
				"next": "/v1/catalog/us/albums/a1/tracks?offset=1",
			})
			return
		}
		writeJSON(t, w, map[string]any{"data": []any{songJSONWithVariants("2", "hi-res-lossless")}})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	tracks, err := p.GetAlbumTracks(context.Background(), "a1")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(tracks) != 2 || !slices.Equal(tracks[1].AudioVariants, []string{"hi-res-lossless"}) {
		t.Errorf("tracks = %+v", tracks)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(extends, []string{"audioVariants", "audioVariants"}) {
		t.Errorf("extend per page = %v, want audioVariants on both", extends)
	}
}
