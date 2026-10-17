package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if lines[0].Words[1].Text != "up" || lines[0].Words[1].Start != 10500*time.Millisecond {
		t.Errorf("word 1 = %+v, want {Start:10.5s Text:up}", lines[0].Words[1])
	}
	if lines[1].Words != nil {
		t.Errorf("line 1 Words = %v, want nil for plain LRC line", lines[1].Words)
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
