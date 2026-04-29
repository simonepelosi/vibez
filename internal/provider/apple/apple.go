package apple

import (
	"bytes"
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

const (
	defaultBaseURL    = "https://api.music.apple.com/v1"
	defaultCatalogURL = "https://amp-api.music.apple.com/v1" // used for catalog search: returns extendedAssetUrls
)

type AppleProvider struct {
	cfg            *config.Config
	client         *http.Client
	baseURL        string
	catalogBaseURL string
	sf             string // cached storefront, populated on first use
}

func New(cfg *config.Config) *AppleProvider {
	return &AppleProvider{
		cfg:            cfg,
		client:         &http.Client{Timeout: 15 * time.Second},
		baseURL:        defaultBaseURL,
		catalogBaseURL: defaultCatalogURL,
	}
}

type storefrontResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// storefront returns the user's Apple Music storefront ID.
// If the config has an explicit override it is used directly; otherwise the
// value reported by Apple's /v1/me/storefront endpoint is fetched and cached.
func (a *AppleProvider) storefront(ctx context.Context) (string, error) {
	if a.cfg.StoreFront != "" {
		return a.cfg.StoreFront, nil
	}
	if a.sf != "" {
		return a.sf, nil
	}
	req, err := a.newRequest(ctx, http.MethodGet, "/me/storefront")
	if err != nil {
		return "", fmt.Errorf("storefront request: %w", err)
	}
	var resp storefrontResponse
	if err := a.do(req, &resp); err != nil {
		return "", fmt.Errorf("storefront: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("storefront: empty response from Apple")
	}
	a.sf = resp.Data[0].ID
	return a.sf, nil
}

func (a *AppleProvider) Name() string { return "apple" }

// SetBaseURL overrides the API base URL. Intended for use in tests only.
// It also sets catalogBaseURL to the same value so both sub-requests hit the
// same test server.
func (a *AppleProvider) SetBaseURL(url string) {
	a.baseURL = url
	a.catalogBaseURL = url
}

func (a *AppleProvider) IsAuthenticated() bool {
	return a.cfg.AppleDeveloperToken != "" && a.cfg.AppleUserToken != ""
}

func (a *AppleProvider) newRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+endpoint, nil) //nolint:gosec // G107: URL is constructed from config, not user input
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	return req, nil
}

// newCatalogRequest builds a request against the catalog (amp-api) base URL.
// amp-api.music.apple.com is the endpoint used by the Apple Music web player;
// unlike the standard API it returns extendedAssetUrls in search responses,
// which lets us reliably detect purchase-only / region-locked tracks.
func (a *AppleProvider) newCatalogRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.catalogBaseURL+endpoint, nil) //nolint:gosec // G107: URL is constructed from config, not user input
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	req.Header.Set("Origin", "https://music.apple.com")
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
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("apple music api %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if dst == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
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

// extendedAssetURLs is returned when the catalog search is made with
// extend=extendedAssetUrls. Its presence indicates that the song can actually
// be streamed with an Apple Music subscription. Songs available only for
// purchase (or unavailable in the user's storefront) will have this field nil.
type extendedAssetURLs struct {
	Plus             string `json:"plus"`
	HLSMediaPlaylist string `json:"hlsMediaPlaylist"`
	EnhancedHLS      string `json:"enhancedHls"`
	LightTunnel      string `json:"lightTunnel"`
}

func (e *extendedAssetURLs) hasStream() bool {
	// Only the `plus` field signals that a song is streamable with an Apple
	// Music subscription. The other fields (hlsMediaPlaylist, enhancedHls,
	// lightTunnel) may be present for purchase-only or preview-only tracks
	// that will still fail with CONTENT_RESTRICTED at playback time.
	return e != nil && e.Plus != ""
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
	GenreNames        []string           `json:"genreNames"`
	PlayParams        *playParams        `json:"playParams"`
	ExtendedAssetURLs *extendedAssetURLs `json:"extendedAssetUrls"`
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
	PlayParams *playParams  `json:"playParams"`
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
	a := provider.Album{
		ID:         r.ID,
		Title:      r.Attributes.Name,
		Artist:     r.Attributes.ArtistName,
		ArtworkURL: r.Attributes.Artwork.formatted(300),
		TrackCount: r.Attributes.TrackCount,
	}
	if r.Attributes.PlayParams != nil && r.Attributes.PlayParams.CatalogID != "" {
		a.CatalogID = r.Attributes.PlayParams.CatalogID
	}
	return a
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

// Search returns results from both the user's library and the catalog.
// Library tracks appear first (guaranteed playable); catalog tracks follow,
// deduplicated by (artist, title).  Any catalog track that MusicKit.js
// cannot resolve is silently skipped at enqueue time — not shown as an error.
func (a *AppleProvider) Search(ctx context.Context, query string) (*provider.SearchResult, error) {
	type libOut struct {
		songs     []songResource
		playlists []playlistResource
		err       error
	}
	type catSongsOut struct {
		songs []songResource
		err   error
	}
	type catCollOut struct {
		albums    []albumResource
		playlists []playlistResource
		err       error
	}

	libCh := make(chan libOut, 1)
	catSongsCh := make(chan catSongsOut, 1)
	catCollCh := make(chan catCollOut, 1)

	// Library: songs the user owns (guaranteed playable) + user playlists.
	// Library albums are intentionally excluded: a library album only contains
	// tracks the user has added, which may be a subset of the full release.
	// Albums come exclusively from the catalog search below.
	go func() {
		ep := fmt.Sprintf("/me/library/search?term=%s&types=library-songs,library-playlists&limit=25",
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
			playlists: resp.Results.Playlists.Data,
		}
	}()

	// Catalog songs via amp-api: returns extendedAssetUrls so we can filter
	// purchase-only / region-locked tracks before they reach the queue.
	go func() {
		sf, err := a.storefront(ctx)
		if err != nil {
			catSongsCh <- catSongsOut{err: err}
			return
		}
		ep := fmt.Sprintf("/catalog/%s/search?term=%s&types=songs&limit=25&extend=extendedAssetUrls",
			sf, url.QueryEscape(query))
		req, err := a.newCatalogRequest(ctx, http.MethodGet, ep)
		if err != nil {
			catSongsCh <- catSongsOut{err: err}
			return
		}
		var resp searchResponse
		if err := a.do(req, &resp); err != nil {
			catSongsCh <- catSongsOut{err: err}
			return
		}
		catSongsCh <- catSongsOut{songs: resp.Results.Songs.Data}
	}()

	// Catalog albums + playlists via the standard API.
	// amp-api.music.apple.com is a web-player endpoint that only reliably
	// returns songs; albums and playlists must be fetched from the standard
	// api.music.apple.com catalog endpoint.
	go func() {
		sf, err := a.storefront(ctx)
		if err != nil {
			catCollCh <- catCollOut{err: err}
			return
		}
		ep := fmt.Sprintf("/catalog/%s/search?term=%s&types=albums,playlists&limit=10",
			sf, url.QueryEscape(query))
		req, err := a.newRequest(ctx, http.MethodGet, ep)
		if err != nil {
			catCollCh <- catCollOut{err: err}
			return
		}
		var resp searchResponse
		if err := a.do(req, &resp); err != nil {
			catCollCh <- catCollOut{err: err}
			return
		}
		catCollCh <- catCollOut{
			albums:    resp.Results.Albums.Data,
			playlists: resp.Results.Playlists.Data,
		}
	}()

	lib := <-libCh
	catSongs := <-catSongsCh
	catColl := <-catCollCh

	if lib.err != nil && catSongs.err != nil && catColl.err != nil {
		return nil, fmt.Errorf("search: lib=%v cat=%v coll=%v", lib.err, catSongs.err, catColl.err)
	}

	result := &provider.SearchResult{}

	// Library songs first — guaranteed playable.
	seen := make(map[string]bool)
	if lib.err == nil {
		for _, s := range lib.songs {
			t := toTrack(s)
			result.Tracks = append(result.Tracks, t)
			seen[strings.ToLower(t.Artist)+"§"+strings.ToLower(t.Title)] = true
		}
		for _, r := range lib.playlists {
			result.Playlists = append(result.Playlists, toPlaylist(r))
		}
	}

	// Catalog songs — skip duplicates already covered by library.
	// Songs without PlayParams are radio-only / unavailable and will always fail
	// in MusicKit, so we drop them here before they can pollute the queue.
	// We also require kind == "song" to exclude music videos, radio episodes, etc.
	// Finally, we require extendedAssetUrls to be present and non-empty: songs
	// that lack streaming URLs are purchase-only or unavailable in the user's
	// storefront, so they would fail on playback too.
	if catSongs.err == nil {
		for _, s := range catSongs.songs {
			if s.Attributes.PlayParams == nil {
				continue
			}
			if s.Attributes.PlayParams.Kind != "song" {
				continue
			}
			if !s.Attributes.ExtendedAssetURLs.hasStream() {
				continue
			}
			t := toTrack(s)
			key := strings.ToLower(t.Artist) + "§" + strings.ToLower(t.Title)
			if !seen[key] {
				result.Tracks = append(result.Tracks, t)
				seen[key] = true
			}
		}
	}

	// Catalog albums + playlists — deduplicate against library playlists.
	if catColl.err == nil {
		for _, r := range catColl.albums {
			result.Albums = append(result.Albums, toAlbum(r))
		}

		seenPlaylists := make(map[string]bool, len(result.Playlists))
		for _, pl := range result.Playlists {
			seenPlaylists[pl.ID] = true
		}
		for _, r := range catColl.playlists {
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
	sf, err := a.storefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetAlbumTracks: %w", err)
	}
	var tracks []provider.Track
	endpoint := fmt.Sprintf("/catalog/%s/albums/%s/tracks?limit=100", sf, albumID)
	for endpoint != "" {
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedSongs
		if err := a.do(req, &page); err != nil {
			return nil, fmt.Errorf("GetAlbumTracks: %w", err)
		}
		for _, s := range page.Data {
			tracks = append(tracks, toTrack(s))
		}
		endpoint = page.Next
	}
	return tracks, nil
}

func (a *AppleProvider) GetLibraryAlbumTracks(ctx context.Context, albumID string) ([]provider.Track, error) {
	var tracks []provider.Track
	endpoint := fmt.Sprintf("/me/library/albums/%s/tracks?limit=100", albumID)
	for endpoint != "" {
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedSongs
		if err := a.do(req, &page); err != nil {
			return nil, fmt.Errorf("GetLibraryAlbumTracks: %w", err)
		}
		for _, s := range page.Data {
			tracks = append(tracks, toTrack(s))
		}
		endpoint = page.Next
	}
	return tracks, nil
}

func (a *AppleProvider) GetCatalogPlaylistTracks(ctx context.Context, playlistID string) ([]provider.Track, error) {
	sf, err := a.storefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetCatalogPlaylistTracks: %w", err)
	}
	var tracks []provider.Track
	endpoint := fmt.Sprintf("/catalog/%s/playlists/%s/tracks?limit=100", sf, playlistID)
	for endpoint != "" {
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedSongs
		if err := a.do(req, &page); err != nil {
			return nil, fmt.Errorf("GetCatalogPlaylistTracks: %w", err)
		}
		for _, s := range page.Data {
			tracks = append(tracks, toTrack(s))
		}
		endpoint = page.Next
	}
	return tracks, nil
}

// createPlaylistRequest is the JSON body for POST /v1/me/library/playlists.
type createPlaylistRequest struct {
	Attributes    createPlaylistAttributes     `json:"attributes"`
	Relationships *createPlaylistRelationships `json:"relationships,omitempty"`
}

type createPlaylistAttributes struct {
	Name string `json:"name"`
}

type createPlaylistRelationships struct {
	Tracks createPlaylistTracks `json:"tracks"`
}

type createPlaylistTracks struct {
	Data []createPlaylistTrackRef `json:"data"`
}

type createPlaylistTrackRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// createPlaylistResponse wraps the newly created playlist.
type createPlaylistResponse struct {
	Data []playlistResource `json:"data"`
}

func (a *AppleProvider) CreatePlaylist(ctx context.Context, name string, trackIDs []string) (provider.Playlist, error) {
	refs := make([]createPlaylistTrackRef, 0, len(trackIDs))
	for _, id := range trackIDs {
		typ := "songs"
		if strings.HasPrefix(id, "i.") {
			typ = "library-songs"
		}
		refs = append(refs, createPlaylistTrackRef{ID: id, Type: typ})
	}

	body := createPlaylistRequest{
		Attributes: createPlaylistAttributes{Name: name},
	}
	if len(refs) > 0 {
		body.Relationships = &createPlaylistRelationships{
			Tracks: createPlaylistTracks{Data: refs},
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return provider.Playlist{}, fmt.Errorf("CreatePlaylist: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/me/library/playlists", bytes.NewReader(raw)) //nolint:gosec // G107: URL is constructed from config, not user input
	if err != nil {
		return provider.Playlist{}, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	req.Header.Set("Content-Type", "application/json")

	var resp createPlaylistResponse
	if err := a.do(req, &resp); err != nil {
		return provider.Playlist{}, fmt.Errorf("CreatePlaylist: %w", err)
	}
	// Some API versions return 201 with no body — treat as success without data.
	if len(resp.Data) == 0 {
		return provider.Playlist{Name: name}, nil
	}
	return toPlaylist(resp.Data[0]), nil
}

// ratingRequest is the body for PUT /v1/me/ratings/songs/{id}.
type ratingRequest struct {
	Type       string           `json:"type"`
	Attributes ratingAttributes `json:"attributes"`
}

type ratingAttributes struct {
	Value int `json:"value"` // 1 = love, -1 = dislike, 0 = neutral
}

// LoveSong adds the catalog song to the user's Apple Music library and marks it
// as Loved (rating value = 1). Pass loved=false to remove the rating.
// catalogID must be the Apple Music catalog ID (not a library "i." prefix ID).
func (a *AppleProvider) LoveSong(ctx context.Context, catalogID string, loved bool) error {
	// Add to library first (idempotent — safe to call even if already in library).
	if loved {
		libURL := fmt.Sprintf("%s/me/library?ids[songs]=%s", a.baseURL, url.QueryEscape(catalogID))
		addReq, err := http.NewRequestWithContext(ctx, http.MethodPost, libURL, http.NoBody) //nolint:gosec // G107: URL is constructed from config, not user input
		if err != nil {
			return fmt.Errorf("LoveSong: add to library: %w", err)
		}
		addReq.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
		addReq.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
		if err := a.do(addReq, nil); err != nil {
			// Non-fatal: song may already be in library.
			_ = err
		}
	}

	ratingURL := fmt.Sprintf("%s/me/ratings/songs/%s", a.baseURL, url.QueryEscape(catalogID))

	if !loved {
		// Remove rating (un-love).
		delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, ratingURL, http.NoBody) //nolint:gosec // G107: URL is constructed from config, not user input
		if err != nil {
			return fmt.Errorf("LoveSong: delete rating: %w", err)
		}
		delReq.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
		delReq.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
		return a.do(delReq, nil)
	}

	body := ratingRequest{
		Type:       "ratings",
		Attributes: ratingAttributes{Value: 1},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("LoveSong: marshal: %w", err)
	}
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, ratingURL, bytes.NewReader(raw)) //nolint:gosec // G107: URL is constructed from config, not user input
	if err != nil {
		return fmt.Errorf("LoveSong: %w", err)
	}
	putReq.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	putReq.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	putReq.Header.Set("Content-Type", "application/json")
	return a.do(putReq, nil)
}

// ratingResponse wraps GET /v1/me/ratings/songs/{id}.
type ratingResponse struct {
	Data []struct {
		Attributes struct {
			Value int `json:"value"` // 1=loved, -1=disliked
		} `json:"attributes"`
	} `json:"data"`
}

// GetSongRating returns true when the given catalog song is marked as Loved
// in the user's Apple Music account.
func (a *AppleProvider) GetSongRating(ctx context.Context, catalogID string) (bool, error) {
	ep := fmt.Sprintf("/me/ratings/songs/%s", url.QueryEscape(catalogID))
	req, err := a.newRequest(ctx, http.MethodGet, ep)
	if err != nil {
		return false, fmt.Errorf("GetSongRating: %w", err)
	}
	// Use the raw client so we can distinguish 404 (not rated) from real errors.
	resp, err := a.client.Do(req) //nolint:gosec // G704: URL is constructed from config, not user input
	if err != nil {
		return false, fmt.Errorf("GetSongRating: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if resp.StatusCode >= 400 {
		return false, nil // treat all errors as "not rated" — non-fatal
	}
	body, _ := io.ReadAll(resp.Body)
	var rating ratingResponse
	if err := json.Unmarshal(body, &rating); err != nil {
		return false, nil
	}
	return len(rating.Data) > 0 && rating.Data[0].Attributes.Value == 1, nil
}

// ── Recommendations ────────────────────────────────────────────────────────

type recommendationContent struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // "albums" or "playlists"
	Attributes struct {
		Name        string       `json:"name"`
		ArtistName  string       `json:"artistName"`
		CuratorName string       `json:"curatorName"`
		Artwork     artworkAttrs `json:"artwork"`
	} `json:"attributes"`
}

type recommendationResource struct {
	Attributes struct {
		Title struct {
			StringForDisplay string `json:"stringForDisplay"`
		} `json:"title"`
	} `json:"attributes"`
	Relationships struct {
		Contents struct {
			Data []recommendationContent `json:"data"`
		} `json:"contents"`
	} `json:"relationships"`
}

type recommendationsResponse struct {
	Data []recommendationResource `json:"data"`
}

// GetRecommendations fetches personalised recommendation groups from
// GET /v1/me/recommendations and returns them as provider-level structs.
func (a *AppleProvider) GetRecommendations(ctx context.Context) ([]provider.RecommendationGroup, error) {
	req, err := a.newRequest(ctx, http.MethodGet, "/me/recommendations?limit=10")
	if err != nil {
		return nil, fmt.Errorf("GetRecommendations: %w", err)
	}
	var resp recommendationsResponse
	if err := a.do(req, &resp); err != nil {
		return nil, fmt.Errorf("GetRecommendations: %w", err)
	}

	var groups []provider.RecommendationGroup
	for _, r := range resp.Data {
		title := r.Attributes.Title.StringForDisplay
		if title == "" {
			continue
		}
		var items []provider.RecommendationItem
		for _, c := range r.Relationships.Contents.Data {
			item := provider.RecommendationItem{ID: c.ID, Title: c.Attributes.Name}
			switch c.Type {
			case "albums":
				item.Kind = "album"
				item.Subtitle = c.Attributes.ArtistName
			case "playlists":
				item.Kind = "playlist"
				item.Subtitle = c.Attributes.CuratorName
			default:
				continue
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			continue
		}
		groups = append(groups, provider.RecommendationGroup{Title: title, Items: items})
	}
	return groups, nil
}
