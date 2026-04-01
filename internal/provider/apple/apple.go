package apple

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/provider"
)

const defaultBaseURL = "https://api.music.apple.com/v1"

type AppleProvider struct {
	cfg     *config.Config
	client  *http.Client
	baseURL string
}

func New(cfg *config.Config) *AppleProvider {
	return &AppleProvider{
		cfg:     cfg,
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: defaultBaseURL,
	}
}

func (a *AppleProvider) Name() string { return "apple" }

// SetBaseURL overrides the API base URL. Intended for use in tests only.
func (a *AppleProvider) SetBaseURL(url string) { a.baseURL = url }

func (a *AppleProvider) IsAuthenticated() bool {
	return a.cfg.AppleDeveloperToken != "" && a.cfg.AppleUserToken != ""
}

func (a *AppleProvider) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	return req, nil
}

func (a *AppleProvider) do(req *http.Request, dst any) error {
	resp, err := a.client.Do(req) //nolint:gosec // G704: URL is constructed from config, not user input
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("apple music api %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// --- Apple Music API response types ---

type artworkAttrs struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (a artworkAttrs) formatted(size int) string {
	s := strconv.Itoa(size)
	u := strings.ReplaceAll(a.URL, "{w}", s)
	u = strings.ReplaceAll(u, "{h}", s)
	return u
}

type playParams struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	IsLibrary bool   `json:"isLibrary"`
	CatalogID string `json:"catalogId"` // catalog ID for library songs — use this for playback
}

type songAttributes struct {
	Name       string       `json:"name"`
	ArtistName string       `json:"artistName"`
	AlbumName  string       `json:"albumName"`
	DurationMs int          `json:"durationInMillis"`
	Artwork    artworkAttrs `json:"artwork"`
	Previews   []struct {
		URL string `json:"url"`
	} `json:"previews"`
	GenreNames []string    `json:"genreNames"`
	PlayParams *playParams `json:"playParams"`
}

type songResource struct {
	ID         string         `json:"id"`
	Attributes songAttributes `json:"attributes"`
}

type albumAttributes struct {
	Name       string       `json:"name"`
	ArtistName string       `json:"artistName"`
	Artwork    artworkAttrs `json:"artwork"`
	TrackCount int          `json:"trackCount"`
}

type albumResource struct {
	ID         string          `json:"id"`
	Attributes albumAttributes `json:"attributes"`
}

type playlistAttributes struct {
	Name       string       `json:"name"`
	Artwork    artworkAttrs `json:"artwork"`
	TrackCount int          `json:"trackCount"`
}

type playlistResource struct {
	ID         string             `json:"id"`
	Attributes playlistAttributes `json:"attributes"`
}

type paginatedSongs struct {
	Data []songResource `json:"data"`
	Next string         `json:"next"`
}

type paginatedPlaylists struct {
	Data []playlistResource `json:"data"`
	Next string             `json:"next"`
}

type searchResponse struct {
	Results struct {
		Songs struct {
			Data []songResource `json:"data"`
		} `json:"songs"`
		Albums struct {
			Data []albumResource `json:"data"`
		} `json:"albums"`
		Playlists struct {
			Data []playlistResource `json:"data"`
		} `json:"playlists"`
	} `json:"results"`
}

// librarySearchResponse matches /v1/me/library/search (keys differ from catalog).
type librarySearchResponse struct {
	Results struct {
		Songs struct {
			Data []songResource `json:"data"`
		} `json:"library-songs"`
		Albums struct {
			Data []albumResource `json:"data"`
		} `json:"library-albums"`
		Playlists struct {
			Data []playlistResource `json:"data"`
		} `json:"library-playlists"`
	} `json:"results"`
}

// --- Converters ---

func toTrack(s songResource) provider.Track {
	var preview string
	if len(s.Attributes.Previews) > 0 {
		preview = s.Attributes.Previews[0].URL
	}
	// Keep the original resource ID. Library songs have IDs like "i.AbCdEfGh";
	// catalog songs (search results) have numeric IDs like "1234567890".
	// The JS layer detects library IDs by the "i." prefix and uses the
	// /v1/me/library/songs/ URL queue format, which plays the full owned track.
	// Catalog IDs use { song: id } which triggers the preview limit if the
	// user doesn't own the track — so we never map library IDs to catalog IDs.
	t := provider.Track{
		ID:         s.ID,
		Title:      s.Attributes.Name,
		Artist:     s.Attributes.ArtistName,
		Album:      s.Attributes.AlbumName,
		Duration:   time.Duration(s.Attributes.DurationMs) * time.Millisecond,
		ArtworkURL: s.Attributes.Artwork.formatted(300),
		PreviewURL: preview,
		Genres:     s.Attributes.GenreNames,
	}
	if s.Attributes.PlayParams != nil && s.Attributes.PlayParams.CatalogID != "" {
		t.CatalogID = s.Attributes.PlayParams.CatalogID
	}
	return t
}

func toAlbum(r albumResource) provider.Album {
	return provider.Album{
		ID:         r.ID,
		Title:      r.Attributes.Name,
		Artist:     r.Attributes.ArtistName,
		ArtworkURL: r.Attributes.Artwork.formatted(300),
		TrackCount: r.Attributes.TrackCount,
	}
}

func toPlaylist(r playlistResource) provider.Playlist {
	return provider.Playlist{
		ID:         r.ID,
		Name:       r.Attributes.Name,
		ArtworkURL: r.Attributes.Artwork.formatted(300),
		TrackCount: r.Attributes.TrackCount,
	}
}

// --- Provider interface ---

// Search returns tracks exclusively from the user's library (guaranteed
// playable) and albums/playlists from the catalog (browsed, not directly
// streamed).  Mixing catalog tracks into results causes "no items resolved"
// errors in MusicKit.js because the REST catalog API and the MusicKit
// streaming layer have different regional availability checks.
func (a *AppleProvider) Search(ctx context.Context, query string) (*provider.SearchResult, error) {
	type libOut struct {
		songs     []songResource
		albums    []albumResource
		playlists []playlistResource
		err       error
	}
	type catOut struct {
		albums    []albumResource
		playlists []playlistResource
		err       error
	}

	libCh := make(chan libOut, 1)
	catCh := make(chan catOut, 1)

	// Library: songs the user owns — always playable.
	go func() {
		ep := fmt.Sprintf("/me/library/search?term=%s&types=library-songs,library-albums,library-playlists&limit=25",
			url.QueryEscape(query))
		req, err := a.newRequest(ctx, http.MethodGet, ep)
		if err != nil {
			libCh <- libOut{err: err}
			return
		}
		var resp librarySearchResponse
		if err := a.do(req, &resp); err != nil {
			libCh <- libOut{err: err}
			return
		}
		libCh <- libOut{
			songs:     resp.Results.Songs.Data,
			albums:    resp.Results.Albums.Data,
			playlists: resp.Results.Playlists.Data,
		}
	}()

	// Catalog: albums and playlists only — no tracks.
	go func() {
		ep := fmt.Sprintf("/catalog/%s/search?term=%s&types=albums,playlists&limit=25",
			a.cfg.StoreFront, url.QueryEscape(query))
		req, err := a.newRequest(ctx, http.MethodGet, ep)
		if err != nil {
			catCh <- catOut{err: err}
			return
		}
		var resp searchResponse
		if err := a.do(req, &resp); err != nil {
			catCh <- catOut{err: err}
			return
		}
		catCh <- catOut{
			albums:    resp.Results.Albums.Data,
			playlists: resp.Results.Playlists.Data,
		}
	}()

	lib := <-libCh
	cat := <-catCh

	if lib.err != nil && cat.err != nil {
		return nil, cat.err
	}

	result := &provider.SearchResult{}

	if lib.err == nil {
		for _, s := range lib.songs {
			result.Tracks = append(result.Tracks, toTrack(s))
		}
		for _, r := range lib.albums {
			result.Albums = append(result.Albums, toAlbum(r))
		}
		for _, r := range lib.playlists {
			result.Playlists = append(result.Playlists, toPlaylist(r))
		}
	}

	// Merge catalog albums/playlists (broader discovery), skipping duplicates.
	if cat.err == nil {
		seenAlbums := make(map[string]bool, len(result.Albums))
		for _, al := range result.Albums {
			seenAlbums[al.ID] = true
		}
		for _, r := range cat.albums {
			if !seenAlbums[r.ID] {
				result.Albums = append(result.Albums, toAlbum(r))
			}
		}

		seenPlaylists := make(map[string]bool, len(result.Playlists))
		for _, pl := range result.Playlists {
			seenPlaylists[pl.ID] = true
		}
		for _, r := range cat.playlists {
			if !seenPlaylists[r.ID] {
				result.Playlists = append(result.Playlists, toPlaylist(r))
			}
		}
	}

	return result, nil
}

func (a *AppleProvider) GetLibraryTracks(ctx context.Context) ([]provider.Track, error) {
	var tracks []provider.Track
	endpoint := "/me/library/songs?limit=100"

	for endpoint != "" {
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedSongs
		if err := a.do(req, &page); err != nil {
			return nil, err
		}
		for _, s := range page.Data {
			tracks = append(tracks, toTrack(s))
		}
		endpoint = page.Next
	}
	return tracks, nil
}

func (a *AppleProvider) GetLibraryPlaylists(ctx context.Context) ([]provider.Playlist, error) {
	var playlists []provider.Playlist
	endpoint := "/me/library/playlists?limit=100"

	for endpoint != "" {
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedPlaylists
		if err := a.do(req, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Data {
			playlists = append(playlists, toPlaylist(r))
		}
		endpoint = page.Next
	}
	return playlists, nil
}

func (a *AppleProvider) GetPlaylistTracks(ctx context.Context, playlistID string) ([]provider.Track, error) {
	var tracks []provider.Track
	endpoint := fmt.Sprintf("/me/library/playlists/%s/tracks?limit=100", playlistID)

	for endpoint != "" {
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedSongs
		if err := a.do(req, &page); err != nil {
			return nil, err
		}
		for _, s := range page.Data {
			tracks = append(tracks, toTrack(s))
		}
		endpoint = page.Next
	}
	return tracks, nil
}

func (a *AppleProvider) GetAlbumTracks(ctx context.Context, albumID string) ([]provider.Track, error) {
	endpoint := fmt.Sprintf("/catalog/%s/albums/%s/tracks?limit=100", a.cfg.StoreFront, albumID)
	req, err := a.newRequest(ctx, http.MethodGet, endpoint)
	if err != nil {
		return nil, err
	}
	var page paginatedSongs
	if err := a.do(req, &page); err != nil {
		return nil, err
	}
	tracks := make([]provider.Track, 0, len(page.Data))
	for _, s := range page.Data {
		tracks = append(tracks, toTrack(s))
	}
	return tracks, nil
}
