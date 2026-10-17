package apple_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func artistJSON(id, name string) map[string]any {
	return map[string]any{"id": id, "attributes": map[string]any{"name": name}}
}

func datedAlbumJSON(id, name, released string) map[string]any {
	a := albumJSON(id, name, "Artist", 10)
	a["attributes"].(map[string]any)["releaseDate"] = released
	return a
}

func TestSearch_ArtistsCappedAtFive(t *testing.T) {
	var artists []any
	for _, n := range []string{"A1", "A2", "A3", "A4", "A5", "A6", "A7"} {
		artists = append(artists, artistJSON(n, "Artist "+n))
	}
	libResp := map[string]any{"results": map[string]any{}}
	catResp := map[string]any{"results": map[string]any{"artists": map[string]any{"data": artists}}}
	var sawArtistsType bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("types"), "artists") {
			sawArtistsType = true
		}
		searchHandler(t, w, r, libResp, catResp)
	}))
	defer srv.Close()

	result, err := newTestProvider(t, srv).Search(context.Background(), "artist")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !sawArtistsType {
		t.Error("no catalog search asked for artists")
	}
	if len(result.Artists) != 5 {
		t.Fatalf("Artists: got %d, want 5", len(result.Artists))
	}
	if a := result.Artists[0]; a.ID != "A1" || a.Name != "Artist A1" {
		t.Errorf("Artists[0] = %+v, want A1 / Artist A1 (Apple's order kept)", a)
	}
}

func TestGetArtistAlbums_PaginatesAndSortsNewestFirst(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Query().Get("offset") == "" {
			writeJSON(t, w, map[string]any{
				"data": []any{datedAlbumJSON("old", "Debut", "2001-05-01"), datedAlbumJSON("yr", "Year Only", "2010")},
				"next": "/v1/catalog/us/artists/42/albums?offset=2",
			})
			return
		}
		writeJSON(t, w, map[string]any{
			"data": []any{datedAlbumJSON("new", "Latest", "2024-03-08"), datedAlbumJSON("mid", "Middle", "2010-06-01")},
		})
	}))
	defer srv.Close()

	albums, err := newTestProvider(t, srv).GetArtistAlbums(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "/catalog/us/artists/42/albums") {
		t.Fatalf("requests = %v, want the artist's albums and then the next page", paths)
	}
	var got []string
	for _, a := range albums {
		got = append(got, a.ID)
	}
	if strings.Join(got, ",") != "new,mid,yr,old" {
		t.Errorf("order = %v, want new,mid,yr,old (newest first, across pages)", got)
	}
	if albums[0].ReleaseDate != "2024-03-08" {
		t.Errorf("ReleaseDate = %q, want 2024-03-08", albums[0].ReleaseDate)
	}
}

func TestGetArtistAlbums_FailedPageFailsTheCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "" {
			writeJSON(t, w, map[string]any{
				"data": []any{datedAlbumJSON("a", "A", "2020-01-01")},
				"next": "/v1/catalog/us/artists/42/albums?offset=1",
			})
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	albums, err := newTestProvider(t, srv).GetArtistAlbums(context.Background(), "42")
	if err == nil {
		t.Fatalf("GetArtistAlbums = %d album(s), nil error; want an error, not a list that looks complete", len(albums))
	}
}

func TestGetArtistAlbums_EndlessNextIsBounded(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		writeJSON(t, w, map[string]any{
			"data": []any{datedAlbumJSON("a", "A", "2020-01-01")},
			"next": "/v1/catalog/us/artists/42/albums?offset=1",
		})
	}))
	defer srv.Close()

	if _, err := newTestProvider(t, srv).GetArtistAlbums(context.Background(), "42"); err == nil {
		t.Fatal("GetArtistAlbums = nil error; want an error once the page cap is hit")
	}
	if requests != 20 {
		t.Errorf("requests = %d, want 20 (the page cap)", requests)
	}
}
