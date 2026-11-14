package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Lyrics come from a community database and are printed in the terminal, so a
// line must never carry an escape sequence through.
func TestParseLRCStripsControlSequences(t *testing.T) {
	lines, err := parseLRC("[00:01.00] hello \x1b[2Jworld\x1b]0;pwned\x07\n[00:02.50] \u202edrawkcab")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, l := range lines {
		if strings.ContainsAny(l.Text, "\x1b\x07\u202e") {
			t.Errorf("line %q still carries a control or override character", l.Text)
		}
	}
	if lines[0].Text != "hello [2Jworld]0;pwned" {
		t.Errorf("line 0 = %q", lines[0].Text)
	}
}

func TestParseLRCEnhancedWordTimestamps(t *testing.T) {
	raw := "[00:10.00] <00:10.00>We're <00:10.50>up <00:11.00>all <00:11.50>night\n[00:13.00] plain line without word tags"
	lines, err := parseLRC(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Text != "We're up all night" {
		t.Errorf("line 0 Text = %q, want %q", lines[0].Text, "We're up all night")
	}
	if len(lines[0].Words) != 4 {
		t.Fatalf("line 0 Words len = %d, want 4", len(lines[0].Words))
	}
	if lines[0].Words[1].Text != "up " || lines[0].Words[1].Start != 10500*time.Millisecond {
		t.Errorf("word 1 = %+v, want {Start:10.5s Text:\"up \"}", lines[0].Words[1])
	}
	if lines[1].Words != nil {
		t.Errorf("line 1 Words = %v, want nil for plain LRC line", lines[1].Words)
	}
}

func TestParseLRCSyllableTimestampsPreserveWords(t *testing.T) {
	raw := "[00:01.00] <00:01.00>Hel<00:01.20>lo <00:01.50>world"
	lines, err := parseLRC(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Text != "Hello world" {
		t.Errorf("line 0 Text = %q, want %q", lines[0].Text, "Hello world")
	}
	if len(lines[0].Words) != 3 {
		t.Fatalf("line 0 Words len = %d, want 3", len(lines[0].Words))
	}
	if lines[0].Words[0].Text != "Hel" || lines[0].Words[1].Text != "lo " || lines[0].Words[2].Text != "world" {
		t.Errorf("words = %+v, want [\"Hel\", \"lo \", \"world\"]", lines[0].Words)
	}
}

func TestCleanSearchTerm(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Get Lucky (feat. Pharrell Williams) [Radio Edit]", "Get Lucky"},
		{"Random Access Memories (10th Anniversary Edition)", "Random Access Memories"},
		{"Veridis Quo - Single", "Veridis Quo"},
		{"Daft Punk", "Daft Punk"},
	}
	for _, tc := range cases {
		if got := cleanSearchTerm(tc.in); got != tc.want {
			t.Errorf("cleanSearchTerm(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFetchFallsBackToSearchOn404(t *testing.T) {
	var searchTrack, searchArtist string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			w.WriteHeader(http.StatusNotFound)
		case "/api/search":
			searchTrack = r.URL.Query().Get("track_name")
			searchArtist = r.URL.Query().Get("artist_name")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"trackName":"Get Lucky","artistName":"Daft Punk","duration":248,"plainLyrics":"plain only"},
				{"trackName":"Get Lucky","artistName":"Daft Punk","duration":248,"syncedLyrics":"[00:01.00] Like the legend of the phoenix"}
			]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.baseURL = srv.URL

	res, err := c.Fetch(
		context.Background(),
		"Daft Punk (feat. Pharrell Williams)",
		"Get Lucky (Radio Edit) - Single",
		"Random Access Memories (Deluxe Edition)",
		248*time.Second,
	)
	if err != nil {
		t.Fatalf("Fetch() unexpected error: %v", err)
	}
	if searchTrack != "Get Lucky" || searchArtist != "Daft Punk" {
		t.Errorf("search query = (%q, %q), want (Get Lucky, Daft Punk)", searchTrack, searchArtist)
	}
	if !res.Synced || len(res.Lines) != 1 || res.Lines[0].Text != "Like the legend of the phoenix" {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestFetchUsesPositiveAndNegativeCache(t *testing.T) {
	var hits, searchHits, serverErrHits atomic.Int32
	failMode := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			if failMode.Load() {
				serverErrHits.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			switch r.URL.Query().Get("track_name") {
			case "Cached Track":
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"trackName":"Cached Track","artistName":"Artist","plainLyrics":"hello"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		case "/api/search":
			if failMode.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			searchHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.baseURL = srv.URL

	// Positive result cached on second Fetch.
	res1, err := c.Fetch(context.Background(), "Artist", "Cached Track", "Album", 180*time.Second)
	if err != nil || res1 == nil {
		t.Fatalf("first Fetch failed: res=%v err=%v", res1, err)
	}
	res2, err := c.Fetch(context.Background(), " artist ", "CACHED TRACK", "album", 180*time.Second)
	if err != nil || res2 != res1 {
		t.Fatalf("second Fetch did not return cached pointer: err=%v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("/api/get hits = %d, want 1", hits.Load())
	}

	// 404 + empty search is negative-cached within TTL.
	_, err1 := c.Fetch(context.Background(), "Nobody", "Missing Song", "", 100*time.Second)
	_, err2 := c.Fetch(context.Background(), "Nobody", "Missing Song", "", 100*time.Second)
	if err1 == nil || err2 == nil {
		t.Fatalf("expected missing lyrics error, got (%v, %v)", err1, err2)
	}
	if searchHits.Load() != 1 {
		t.Fatalf("search hits on negative-cached miss = %d, want 1", searchHits.Load())
	}

	// Transient 5xx errors must not be negative-cached.
	failMode.Store(true)
	_, _ = c.Fetch(context.Background(), "ErrArtist", "ErrSong", "", 120*time.Second)
	_, _ = c.Fetch(context.Background(), "ErrArtist", "ErrSong", "", 120*time.Second)
	if serverErrHits.Load() != 2 {
		t.Fatalf("5xx error calls = %d, want 2 (never negative-cached)", serverErrHits.Load())
	}
}
