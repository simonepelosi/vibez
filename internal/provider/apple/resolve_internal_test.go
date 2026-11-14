package apple

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/provider"
)

func TestResolveURL(t *testing.T) {
	const base = "https://api.music.apple.com/v1"
	ok := map[string]string{
		"/me/library/songs":                  base + "/me/library/songs",
		"/v1/me/library/songs?offset=100":    base + "/me/library/songs?offset=100",
		base + "/me/library/songs?offset=25": base + "/me/library/songs?offset=25",
		"HTTPS://API.MUSIC.APPLE.COM/v1/me":  "HTTPS://API.MUSIC.APPLE.COM/v1/me",
	}
	for in, want := range ok {
		got, err := resolveURL(base, in)
		if err != nil || got != want {
			t.Errorf("resolveURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"",
		"https://evil.example/v1/me/library/songs",
		"HTTPS://evil.example/v1/me/library/songs",
		"http://api.music.apple.com/v1/me/library/songs",
		"https://api.music.apple.com.evil.example/v1/x",
		"https://api.music.apple.com@evil.example/v1/x",
		"https://user:pass@api.music.apple.com/v1/x",
		"//evil.example/v1/me/library/songs",
		"@evil.example/v1/x",
		"me/library/songs",
		"/me/library/songs\r\nX-Injected: 1",
		"/me/library/songs with space",
	} {
		if got, err := resolveURL(base, in); err == nil {
			t.Errorf("resolveURL(%q) = %q, want refused", in, got)
		}
	}

	// Host-only base URL must still reject @evil authority confusion.
	if got, err := resolveURL("https://api.music.apple.com", "@evil.example/x"); err == nil {
		t.Errorf("resolveURL with @evil on pathless base = %q, want refused", got)
	}
}

func TestValidateStorefront(t *testing.T) {
	for _, valid := range []string{"us", "it", "gb", "en-gb", "zh-Hant-TW"} {
		if got, err := validateStorefront(valid); err != nil || got != valid {
			t.Errorf("validateStorefront(%q) = %q, %v; want valid", valid, got, err)
		}
	}
	for _, bad := range []string{
		"",
		"a",
		"../me/library",
		"us/../../evil",
		"us?foo=bar",
		"us#frag",
		"us%2f..",
		"toolongstorefrontcode123",
	} {
		if got, err := validateStorefront(bad); err == nil {
			t.Errorf("validateStorefront(%q) = %q, want error", bad, got)
		}
	}
}

func TestCrossOriginRedirectBlocked_NeverLeaksTokens(t *testing.T) {
	var leakedAuth, leakedUserToken atomic.Bool
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leakedAuth.Store(true)
		}
		if r.Header.Get("Music-User-Token") != "" {
			leakedUserToken.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/collect", http.StatusFound)
	}))
	defer origin.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "secret-dev-token",
		AppleUserToken:      "secret-user-token",
		StoreFront:          "us",
	})
	p.SetBaseURL(origin.URL)

	_, err := p.GetLibraryTracks(context.Background())
	if err == nil {
		t.Fatal("expected cross-origin redirect to fail, got nil")
	}
	if leakedAuth.Load() || leakedUserToken.Load() {
		t.Fatalf("credentials leaked to cross-origin redirect target (auth=%v, userToken=%v)", leakedAuth.Load(), leakedUserToken.Load())
	}
}

func TestPagination_RespectsServerSidePageCap25(t *testing.T) {
	const totalItems = 80
	const serverCap = 25

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		end := min(offset+serverCap, totalItems)
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/me/library/playlists":
			var data []map[string]any
			for i := offset; i < end; i++ {
				data = append(data, map[string]any{
					"id": fmt.Sprintf("p.%03d", i),
					"attributes": map[string]any{
						"name":       fmt.Sprintf("Playlist %03d", i),
						"trackCount": 10,
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": data,
				"meta": map[string]any{"total": totalItems},
			})
		case "/me/library/songs":
			var data []map[string]any
			for i := offset; i < end; i++ {
				data = append(data, map[string]any{
					"id": fmt.Sprintf("i.%03d", i),
					"attributes": map[string]any{
						"name":             fmt.Sprintf("Song %03d", i),
						"artistName":       "Artist",
						"albumName":        "Album",
						"durationInMillis": 180000,
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": data,
				"meta": map[string]any{"total": totalItems},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
	})
	p.SetBaseURL(srv.URL)

	playlists, err := p.GetLibraryPlaylists(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryPlaylists: %v", err)
	}
	// +1 for synthetic Favorites playlist prepended when none is named "Favorites".
	if len(playlists) != totalItems+1 {
		t.Fatalf("got %d playlists, want %d", len(playlists), totalItems+1)
	}
	for i := range totalItems {
		wantID := fmt.Sprintf("p.%03d", i)
		if playlists[i+1].ID != wantID {
			t.Fatalf("playlist[%d].ID = %q, want %q", i+1, playlists[i+1].ID, wantID)
		}
	}

	tracks, err := p.GetLibraryTracks(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryTracks: %v", err)
	}
	if len(tracks) != totalItems {
		t.Fatalf("got %d tracks, want %d", len(tracks), totalItems)
	}
	for i := range totalItems {
		wantID := fmt.Sprintf("i.%03d", i)
		if tracks[i].ID != wantID {
			t.Fatalf("track[%d].ID = %q, want %q", i, tracks[i].ID, wantID)
		}
	}
}

func TestPagination_HostileMetaAndSelfReferentialCursor(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me/library/songs":
			// Negative meta.total + self-referential next cursor must not panic or loop forever.
			n := calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": fmt.Sprintf("i.%d", n), "attributes": map[string]any{"name": "S"}},
				},
				"next": "/me/library/songs?offset=0",
				"meta": map[string]any{"total": -42},
			})
		case "/me/library/playlists":
			// Positive meta.total with empty data must not infinite-loop on pageSize == 0.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{},
				"meta": map[string]any{"total": 50},
			})
		}
	}))
	defer srv.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
	})
	p.SetBaseURL(srv.URL)

	tracks, err := p.GetLibraryTracks(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryTracks: %v", err)
	}
	if len(tracks) == 0 || calls.Load() > 3 {
		t.Fatalf("self-referential cursor calls = %d, tracks = %d", calls.Load(), len(tracks))
	}

	playlists, err := p.GetLibraryPlaylists(context.Background())
	if err != nil {
		t.Fatalf("GetLibraryPlaylists: %v", err)
	}
	if len(playlists) != 1 { // only synthetic Favorites
		t.Fatalf("playlists = %d, want 1", len(playlists))
	}
}

func TestGetPlaylistTracks_FallbackPaginatesRelationshipNext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/me/library/playlists/p.1/tracks" && r.URL.Query().Get("offset") == "":
			http.NotFound(w, r)
		case r.URL.Path == "/me/library/playlists/p.1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id": "p.1",
						"relationships": map[string]any{
							"tracks": map[string]any{
								"data": []map[string]any{
									{"id": "i.1", "attributes": map[string]any{"name": "First"}},
								},
								"next": "/v1/me/library/playlists/p.1/tracks?offset=1",
							},
						},
					},
				},
			})
		case r.URL.Path == "/me/library/playlists/p.1/tracks" && r.URL.Query().Get("offset") == "1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "i.2", "attributes": map[string]any{"name": "Second"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
	})
	p.SetBaseURL(srv.URL)

	tracks, err := p.GetPlaylistTracks(context.Background(), "p.1")
	if err != nil {
		t.Fatalf("GetPlaylistTracks: %v", err)
	}
	if len(tracks) != 2 || tracks[0].ID != "i.1" || tracks[1].ID != "i.2" {
		t.Fatalf("tracks = %+v, want [i.1, i.2]", tracks)
	}
}

func TestDo_Retries429And503Safely(t *testing.T) {
	var getCalls, post429Calls, post503Calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/catalog/us/albums/a1/tracks":
			if getCalls.Add(1) < 3 {
				w.Header().Set("Retry-After", "0")
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "10", "attributes": map[string]any{"name": "Recovered"}},
				},
			})
		case "/me/library/playlists":
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "My Playlist") {
				t.Errorf("POST retry lost request body: %q", string(raw))
			}
			if r.URL.Query().Get("fail503") == "1" {
				post503Calls.Add(1)
				http.Error(w, "bad gateway", http.StatusServiceUnavailable)
				return
			}
			if post429Calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "0")
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "p.new", "attributes": map[string]any{"name": "My Playlist"}},
				},
			})
		}
	}))
	defer srv.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
	})
	p.SetBaseURL(srv.URL)

	tracks, err := p.GetAlbumTracks(context.Background(), "a1")
	if err != nil || len(tracks) != 1 || getCalls.Load() != 3 {
		t.Fatalf("GET retry: tracks=%+v err=%v calls=%d", tracks, err, getCalls.Load())
	}

	pl, err := p.CreatePlaylist(context.Background(), "My Playlist", nil)
	if err != nil || pl.ID != "p.new" || post429Calls.Load() != 2 {
		t.Fatalf("POST 429 retry: pl=%+v err=%v calls=%d", pl, err, post429Calls.Load())
	}

	// Non-idempotent POST on 503 must NOT retry to avoid duplicate playlist creation.
	req, err := p.newRequestWithBody(context.Background(), http.MethodPost, "/me/library/playlists?fail503=1", "application/json", []byte(`{"attributes":{"name":"My Playlist"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.do(req, nil); err == nil || post503Calls.Load() != 1 {
		t.Fatalf("POST 503 should fail immediately without retry: err=%v calls=%d", err, post503Calls.Load())
	}

	// Huge Retry-After must be clamped to maxRetryAfter.
	if got := parseRetryAfter("999999", time.Millisecond); got != maxRetryAfter {
		t.Errorf("parseRetryAfter(999999) = %v, want %v", got, maxRetryAfter)
	}
}

func TestStorefront_ConcurrentSearchRaceFree(t *testing.T) {
	var sfCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me/storefront":
			sfCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "it"}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{}})
		}
	}))
	defer srv.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "", // force lazy /me/storefront fetch under concurrency
	})
	p.SetBaseURL(srv.URL)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := p.Search(context.Background(), "daft punk"); err != nil {
				t.Errorf("Search error: %v", err)
			}
		})
	}
	wg.Wait()

	if sfCalls.Load() != 1 {
		t.Errorf("expected /me/storefront to be fetched once, got %d", sfCalls.Load())
	}
}

func TestSetSongRating_RetriesPutOn429And503WithRewoundBody(t *testing.T) {
	var putCalls atomic.Int32
	var bodies []string
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/me/library":
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == "/me/ratings/songs/12345":
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(b))
			mu.Unlock()
			switch putCalls.Add(1) {
			case 1:
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
			case 2:
				w.WriteHeader(http.StatusServiceUnavailable)
			default:
				w.WriteHeader(http.StatusOK)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
	})
	p.SetBaseURL(srv.URL)
	p.retryBaseDelay = time.Millisecond

	if err := p.SetSongRating(context.Background(), "12345", provider.RatingLoved); err != nil {
		t.Fatalf("SetSongRating(RatingLoved) failed across 429+503 retries: %v", err)
	}
	if got := putCalls.Load(); got != 3 {
		t.Fatalf("PUT attempts = %d, want 3", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, body := range bodies {
		if !strings.Contains(body, `"value":1`) {
			t.Errorf("PUT attempt %d body = %q, want non-empty rating JSON", i+1, body)
		}
	}
}

func TestAppleProvider_HTTPPoolAndLibraryCache(t *testing.T) {
	var tracksCalls, playlistsCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/me/library/songs":
			tracksCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "i.1", "attributes": map[string]any{"name": "Track One", "artistName": "Artist"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/me/library/playlists":
			playlistsCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "p.1", "attributes": map[string]any{"name": "Favorites"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/me/library/playlists":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "p.2", "attributes": map[string]any{"name": "Created"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/me/library":
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == "/me/ratings/songs/123":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	var nowMu sync.Mutex

	p := New(&config.Config{
		AppleDeveloperToken: "dev",
		AppleUserToken:      "usr",
		StoreFront:          "us",
	})
	p.SetBaseURL(srv.URL)
	p.nowFn = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}

	if tr, ok := p.client.Transport.(*http.Transport); !ok || tr.MaxIdleConnsPerHost < 16 {
		t.Fatalf("expected pooled HTTP transport with MaxIdleConnsPerHost >= 16, got %#v", p.client.Transport)
	}

	// First call fetches from API; second call within TTL hits cache and returns a defensive copy.
	t1, err := p.GetLibraryTracks(context.Background())
	if err != nil || len(t1) != 1 {
		t.Fatalf("GetLibraryTracks #1: %v %v", t1, err)
	}
	t1[0].Title = "Mutated By Caller"
	t2, err := p.GetLibraryTracks(context.Background())
	if err != nil || len(t2) != 1 || t2[0].Title != "Track One" {
		t.Fatalf("GetLibraryTracks #2: %v %v", t2, err)
	}
	if tracksCalls.Load() != 1 {
		t.Fatalf("tracks API calls = %d, want 1 (cached)", tracksCalls.Load())
	}

	// Playlists cache hit then CreatePlaylist invalidation.
	pl1, err := p.GetLibraryPlaylists(context.Background())
	if err != nil || len(pl1) != 1 {
		t.Fatalf("GetLibraryPlaylists #1: %v %v", pl1, err)
	}
	if _, err := p.GetLibraryPlaylists(context.Background()); err != nil {
		t.Fatalf("GetLibraryPlaylists #2: %v", err)
	}
	if playlistsCalls.Load() != 1 {
		t.Fatalf("playlists API calls = %d, want 1 (cached)", playlistsCalls.Load())
	}
	if _, err := p.CreatePlaylist(context.Background(), "Created", nil); err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}
	if _, err := p.GetLibraryPlaylists(context.Background()); err != nil {
		t.Fatalf("GetLibraryPlaylists #3: %v", err)
	}
	if playlistsCalls.Load() != 2 {
		t.Fatalf("playlists API calls after CreatePlaylist = %d, want 2", playlistsCalls.Load())
	}

	// SetSongRating(RatingLoved) adds to library -> invalidates tracks cache.
	if err := p.SetSongRating(context.Background(), "123", provider.RatingLoved); err != nil {
		t.Fatalf("SetSongRating: %v", err)
	}
	if _, err := p.GetLibraryTracks(context.Background()); err != nil {
		t.Fatalf("GetLibraryTracks #3: %v", err)
	}
	if tracksCalls.Load() != 2 {
		t.Fatalf("tracks API calls after RatingLoved = %d, want 2", tracksCalls.Load())
	}

	// Advancing past TTL triggers re-fetch cleanly.
	nowMu.Lock()
	now = now.Add(p.cacheTTL + time.Second)
	nowMu.Unlock()
	if _, err := p.GetLibraryTracks(context.Background()); err != nil {
		t.Fatalf("GetLibraryTracks #4: %v", err)
	}
	if tracksCalls.Load() != 3 {
		t.Fatalf("tracks API calls after TTL expiry = %d, want 3", tracksCalls.Load())
	}
}
