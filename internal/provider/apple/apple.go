package apple

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	defaultBaseURL         = "https://api.music.apple.com/v1"
	defaultCatalogURL      = "https://amp-api.music.apple.com/v1" // no longer used by search; see newCatalogRequest
	favoritesPlaylistID    = "vibez:favorites"
	favoritesPlaylistName  = "Favorites"
	ratingBatchSize        = 100
	ratingBatchConcurrency = 5
	// maxResponseBody bounds how much of any API response is read.
	maxResponseBody = 32 << 20
)

// resolveURL turns an endpoint into the URL to request. A relative endpoint
// is joined to base. An absolute one, which is how Apple's "next" pagination
// links may arrive, is accepted only on base's own scheme and host: every
// request carries the developer and user tokens, so it must not be possible
// for a response to steer them to another server.
func resolveURL(base, endpoint string) (string, error) {
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		if strings.HasPrefix(endpoint, "/v1/") {
			endpoint = strings.TrimPrefix(endpoint, "/v1")
		}
		return base + endpoint, nil
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if u.Scheme != b.Scheme || !strings.EqualFold(u.Host, b.Host) {
		return "", fmt.Errorf("refusing to send credentials to %s://%s", u.Scheme, u.Host)
	}
	return endpoint, nil
}

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

// storefront returns the user's Apple Music storefront ID: the one the startup
// session check saved to config, or failing that the value reported by Apple's
// /v1/me/storefront endpoint, fetched once and cached.
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
	u, err := resolveURL(a.baseURL, endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil) //nolint:gosec // G107: URL is constructed from config, not user input
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	return req, nil
}

// newCatalogRequest builds a request against the catalog (amp-api) base URL.
// amp-api.music.apple.com is the endpoint used by the Apple Music web player,
// and the only one that returns extendedAssetUrls in search responses.
//
// Nothing reaches this any more: #82 moved album and playlist tracks to the
// standard host, and #93 moved catalog search there too, so every
// fetchSongsPaginated caller now passes isCatalog=false. Kept rather than
// deleted because removing it is a judgement call about whether vibez is done
// with amp-api, not a consequence of either change.
func (a *AppleProvider) newCatalogRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	u, err := resolveURL(a.catalogBaseURL, endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil) //nolint:gosec // G107: URL is constructed from config, not user input
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
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("request timed out: %w", err)
		}
		return fmt.Errorf("http request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode >= 400 {
		return &apiError{status: resp.StatusCode, msg: formatAPIError(resp.StatusCode, resp.Status, body)}
	}
	if dst == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

// apiError is an HTTP error response from the Apple Music API. It keeps the
// status code so callers can branch on it without parsing the message.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func formatAPIError(statusCode int, status string, body []byte) string {
	msg := fmt.Sprintf("apple music api %s", status)
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" {
		msg += ": " + trimmed
	}
	if statusCode == http.StatusUnauthorized {
		msg += ". Re-run `vibez login`; if you're using a local build, also refresh apple_developer_token."
	}
	return msg
}

// --- Apple Music API response types ---

type artworkAttrs struct {
	URL        string `json:"url"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	BgColor    string `json:"bgColor"`
	TextColor1 string `json:"textColor1"`
	TextColor2 string `json:"textColor2"`
	TextColor3 string `json:"textColor3"`
	TextColor4 string `json:"textColor4"`
}

func (a artworkAttrs) formatted(size int) string {
	s := strconv.Itoa(size)
	u := strings.ReplaceAll(a.URL, "{w}", s)
	u = strings.ReplaceAll(u, "{h}", s)
	return u
}

func (a artworkAttrs) palette() provider.ArtworkPalette {
	return provider.ArtworkPalette{
		BgColor:    normalizeHexColor(a.BgColor),
		TextColor1: normalizeHexColor(a.TextColor1),
		TextColor2: normalizeHexColor(a.TextColor2),
		TextColor3: normalizeHexColor(a.TextColor3),
		TextColor4: normalizeHexColor(a.TextColor4),
	}
}

func normalizeHexColor(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 3 && len(s) != 6 {
		return ""
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return "#" + strings.ToLower(s)
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
	GenreNames           []string    `json:"genreNames"`
	PlayParams           *playParams `json:"playParams"`
	DiscNumber           int         `json:"discNumber"`
	TrackNumber          int         `json:"trackNumber"`
	ContentRating        string      `json:"contentRating"`
	IsAppleDigitalMaster bool        `json:"isAppleDigitalMaster"`
	ReleaseDate          string      `json:"releaseDate"`
	// AudioVariants arrives only when the request asks for
	// extend=audioVariants. AudioTraits is amp-api's name for the same data.
	AudioVariants []string `json:"audioVariants"`
	AudioTraits   []string `json:"audioTraits"`
}

type songResource struct {
	ID         string         `json:"id"`
	Attributes songAttributes `json:"attributes"`
}

type albumAttributes struct {
	Name        string       `json:"name"`
	ArtistName  string       `json:"artistName"`
	Artwork     artworkAttrs `json:"artwork"`
	TrackCount  int          `json:"trackCount"`
	ReleaseDate string       `json:"releaseDate"`
	PlayParams  *playParams  `json:"playParams"`
}

type albumResource struct {
	ID         string          `json:"id"`
	Attributes albumAttributes `json:"attributes"`
}

type artistResource struct {
	ID         string `json:"id"`
	Attributes struct {
		Name string `json:"name"`
	} `json:"attributes"`
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
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
}

type paginatedAlbums struct {
	Data []albumResource `json:"data"`
	Next string          `json:"next"`
}

type paginatedPlaylists struct {
	Data []playlistResource `json:"data"`
	Next string             `json:"next"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
}

type playlistWithTracksResponse struct {
	Data []struct {
		ID            string             `json:"id"`
		Attributes    playlistAttributes `json:"attributes"`
		Relationships struct {
			Tracks struct {
				Data []songResource `json:"data"`
			} `json:"tracks"`
		} `json:"relationships"`
	} `json:"data"`
}

type searchResponse struct {
	Results struct {
		Songs struct {
			Data []songResource `json:"data"`
		} `json:"songs"`
		Albums struct {
			Data []albumResource `json:"data"`
		} `json:"albums"`
		Artists struct {
			Data []artistResource `json:"data"`
		} `json:"artists"`
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
		ID:                   s.ID,
		Title:                provider.Clean(s.Attributes.Name),
		Artist:               provider.Clean(s.Attributes.ArtistName),
		Album:                provider.Clean(s.Attributes.AlbumName),
		Duration:             time.Duration(s.Attributes.DurationMs) * time.Millisecond,
		ArtworkURL:           s.Attributes.Artwork.formatted(300),
		PreviewURL:           preview,
		Genres:               provider.CleanAll(slices.Clone(s.Attributes.GenreNames)),
		DiscNumber:           s.Attributes.DiscNumber,
		TrackNumber:          s.Attributes.TrackNumber,
		ContentRating:        strings.ToLower(strings.TrimSpace(provider.Clean(s.Attributes.ContentRating))),
		IsAppleDigitalMaster: s.Attributes.IsAppleDigitalMaster,
		ReleaseDate:          strings.TrimSpace(provider.Clean(s.Attributes.ReleaseDate)),
		ArtworkPalette:       s.Attributes.Artwork.palette(),
		AudioVariants:        normalizeAudioVariants(s.Attributes.AudioVariants, s.Attributes.AudioTraits),
	}
	if s.Attributes.PlayParams != nil && s.Attributes.PlayParams.CatalogID != "" {
		t.CatalogID = s.Attributes.PlayParams.CatalogID
	}
	return t
}

// normalizeAudioVariants merges the audioVariants and audioTraits lists into
// one lowercase, de-duplicated list in audioVariants' vocabulary, so callers
// need not care which host answered. amp-api says "atmos" where the standard
// API says "dolby-atmos".
func normalizeAudioVariants(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		for _, v := range list {
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "atmos" {
				v = "dolby-atmos"
			}
			if v != "" && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// audioVariantsExtend is the extend value that adds audioVariants to catalog
// song resources on api.music.apple.com. That host rejects amp-api's
// "audioTraits" with a 400. Library resources carry no audio variants, so only
// catalog song requests ask for it.
const audioVariantsExtend = "audioVariants"

// withAudioVariants returns endpoint with extend=audioVariants set. An
// endpoint that does not parse is returned unchanged.
func withAudioVariants(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	q := u.Query()
	q.Set("extend", audioVariantsExtend)
	u.RawQuery = q.Encode()
	return u.String()
}

// withoutAudioVariants undoes withAudioVariants.
func withoutAudioVariants(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	q := u.Query()
	q.Del("extend")
	u.RawQuery = q.Encode()
	return u.String()
}

func hasAudioVariants(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Query().Get("extend") == audioVariantsExtend
}

// isBadRequest reports whether err is an HTTP 400 from do.
func isBadRequest(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.status == http.StatusBadRequest
}

// getExtended fetches endpoint into dst. If the request asked for
// extend=audioVariants and Apple answered 400, it retries once without the
// extension, so an endpoint that refuses it costs the format badge rather
// than the whole call. It returns the endpoint that was used last.
func (a *AppleProvider) getExtended(ctx context.Context, build func(context.Context, string, string) (*http.Request, error), endpoint string, dst any) (string, error) {
	req, err := build(ctx, http.MethodGet, endpoint)
	if err != nil {
		return endpoint, err
	}
	err = a.do(req, dst)
	if !isBadRequest(err) || !hasAudioVariants(endpoint) {
		return endpoint, err
	}
	endpoint = withoutAudioVariants(endpoint)
	req, err = build(ctx, http.MethodGet, endpoint)
	if err != nil {
		return endpoint, err
	}
	return endpoint, a.do(req, dst)
}

func toAlbum(r albumResource) provider.Album {
	a := provider.Album{
		ID:          r.ID,
		Title:       provider.Clean(r.Attributes.Name),
		Artist:      provider.Clean(r.Attributes.ArtistName),
		ArtworkURL:  r.Attributes.Artwork.formatted(300),
		TrackCount:  r.Attributes.TrackCount,
		ReleaseDate: provider.Clean(r.Attributes.ReleaseDate),
	}
	if r.Attributes.PlayParams != nil && r.Attributes.PlayParams.CatalogID != "" {
		a.CatalogID = r.Attributes.PlayParams.CatalogID
	}
	return a
}

func toPlaylist(r playlistResource) provider.Playlist {
	return provider.Playlist{
		ID:         r.ID,
		Name:       provider.Clean(r.Attributes.Name),
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
		artists   []artistResource
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

	// Catalog songs via the standard API, the same host albums and playlists
	// use. amp-api.music.apple.com answered this leg until #118 made it a
	// fallback, because it alone returns extendedAssetUrls. It is an
	// undocumented web-player endpoint, it has now spent months rejecting
	// token pairs the supported endpoints accept, and a leg that 401s leaves
	// discovery with only the user's library to refill from.
	go func() {
		sf, err := a.storefront(ctx)
		if err != nil {
			catSongsCh <- catSongsOut{err: err}
			return
		}
		ep := withAudioVariants(fmt.Sprintf("/catalog/%s/search?term=%s&types=songs&limit=25",
			sf, url.QueryEscape(query)))
		var resp searchResponse
		if _, err := a.getExtended(ctx, a.newRequest, ep, &resp); err != nil {
			catSongsCh <- catSongsOut{err: err}
			return
		}
		catSongsCh <- catSongsOut{songs: resp.Results.Songs.Data}
	}()

	// Catalog albums, artists and playlists via the standard API.
	// amp-api.music.apple.com is a web-player endpoint that only reliably
	// returns songs; albums and playlists must be fetched from the standard
	// api.music.apple.com catalog endpoint.
	go func() {
		sf, err := a.storefront(ctx)
		if err != nil {
			catCollCh <- catCollOut{err: err}
			return
		}
		ep := fmt.Sprintf("/catalog/%s/search?term=%s&types=albums,artists,playlists&limit=10",
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
			artists:   resp.Results.Artists.Data,
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

	// A single failing leg is not fatal: the other two still carry usable
	// results. Record it so callers can report an incomplete search instead of
	// presenting it as an empty one.
	if lib.err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("library search: %v", lib.err))
	}
	if catSongs.err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("catalog song search: %v", catSongs.err))
	}
	if catColl.err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("catalog album/artist/playlist search: %v", catColl.err))
	}

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
	// This is the whole filter: the standard catalog endpoint returns no
	// extendedAssetUrls, so streamability cannot be checked here, and playback
	// is the final authority on storefront availability. Catalog album and
	// playlist tracks have reached the queue on exactly these terms since #82
	// flipped them off amp-api.
	if catSongs.err == nil {
		for _, s := range catSongs.songs {
			if s.Attributes.PlayParams == nil {
				continue
			}
			if s.Attributes.PlayParams.Kind != "song" {
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

	// Catalog albums, artists and playlists. Playlists are deduplicated
	// against library playlists.
	if catColl.err == nil {
		for _, r := range catColl.albums {
			result.Albums = append(result.Albums, toAlbum(r))
		}
		// The search limit applies to every type at once. An artist is a way
		// into their albums rather than a result in itself, so a short list
		// is enough.
		for _, r := range catColl.artists[:min(len(catColl.artists), searchArtistLimit)] {
			result.Artists = append(result.Artists, provider.Artist{ID: r.ID, Name: provider.Clean(r.Attributes.Name)})
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

func (a *AppleProvider) fetchSongsPaginated(ctx context.Context, baseEndpoint string, isCatalog bool) ([]provider.Track, error) {
	reqBuilder := a.newRequest
	if isCatalog {
		reqBuilder = a.newCatalogRequest
	}

	var firstPage paginatedSongs
	baseEndpoint, err := a.getExtended(ctx, reqBuilder, baseEndpoint, &firstPage)
	if err != nil {
		return nil, err
	}
	extended := hasAudioVariants(baseEndpoint)

	total := firstPage.Meta.Total
	if total == 0 {
		tracks := make([]provider.Track, 0, len(firstPage.Data))
		for _, s := range firstPage.Data {
			tracks = append(tracks, toTrack(s))
		}
		if firstPage.Next == "" {
			return tracks, nil
		}
		endpoint := firstPage.Next
		for endpoint != "" {
			// Apple's next links need not repeat the request's extend.
			if extended {
				endpoint = withAudioVariants(endpoint)
			}
			req, err := reqBuilder(ctx, http.MethodGet, endpoint)
			if err != nil {
				return nil, err
			}
			var page paginatedSongs
			if err := a.do(req, &page); err != nil {
				if len(tracks) > 0 && (strings.Contains(err.Error(), "404 Not Found") || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil) {
					break
				}
				if strings.Contains(err.Error(), "404 Not Found") {
					return nil, nil
				}
				return nil, err
			}
			for _, s := range page.Data {
				tracks = append(tracks, toTrack(s))
			}
			endpoint = page.Next
		}
		return tracks, nil
	}

	limit := 100
	tracks := make([]provider.Track, total)
	for i, s := range firstPage.Data {
		if i < total {
			tracks[i] = toTrack(s)
		}
	}

	if total <= len(firstPage.Data) {
		return tracks[:len(firstPage.Data)], nil
	}

	offsets := []int{}
	for offset := len(firstPage.Data); offset < total; offset += limit {
		offsets = append(offsets, offset)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(offsets))
	sem := make(chan struct{}, 10)

	for _, offset := range offsets {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			u, err := url.Parse(baseEndpoint)
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			q := u.Query()
			q.Set("offset", strconv.Itoa(offset))
			q.Set("limit", strconv.Itoa(limit))
			u.RawQuery = q.Encode()

			req, err := reqBuilder(ctx, http.MethodGet, u.String())
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			var page paginatedSongs
			if err := a.do(req, &page); err != nil {
				if strings.Contains(err.Error(), "404 Not Found") {
					return
				}
				select {
				case errChan <- err:
				default:
				}
				return
			}

			for j, s := range page.Data {
				idx := offset + j
				if idx < len(tracks) {
					tracks[idx] = toTrack(s)
				}
			}
		}(offset)
	}

	wg.Wait()

	select {
	case err := <-errChan:
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			var filtered []provider.Track
			for _, t := range tracks {
				if t.ID != "" {
					filtered = append(filtered, t)
				}
			}
			if len(filtered) > 0 {
				return filtered, nil
			}
		}
		return nil, err
	default:
	}

	var finalTracks []provider.Track
	for _, t := range tracks {
		if t.ID != "" {
			finalTracks = append(finalTracks, t)
		}
	}
	return finalTracks, nil
}

func (a *AppleProvider) GetLibraryTracks(ctx context.Context) ([]provider.Track, error) {
	endpoint := "/me/library/songs?limit=100"
	tracks, err := a.fetchSongsPaginated(ctx, endpoint, false)
	if err != nil {
		if strings.Contains(err.Error(), "404 Not Found") {
			return nil, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (a *AppleProvider) fetchPlaylistsPaginated(ctx context.Context, baseEndpoint string) ([]provider.Playlist, error) {
	req, err := a.newRequest(ctx, http.MethodGet, baseEndpoint)
	if err != nil {
		return nil, err
	}
	var firstPage paginatedPlaylists
	if err := a.do(req, &firstPage); err != nil {
		return nil, err
	}

	total := firstPage.Meta.Total
	if total == 0 {
		playlists := make([]provider.Playlist, 0, len(firstPage.Data))
		for _, r := range firstPage.Data {
			playlists = append(playlists, toPlaylist(r))
		}
		if firstPage.Next == "" {
			return playlists, nil
		}
		endpoint := firstPage.Next
		for endpoint != "" {
			req, err := a.newRequest(ctx, http.MethodGet, endpoint)
			if err != nil {
				return nil, err
			}
			var page paginatedPlaylists
			if err := a.do(req, &page); err != nil {
				if len(playlists) > 0 && (strings.Contains(err.Error(), "404 Not Found") || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil) {
					break
				}
				if strings.Contains(err.Error(), "404 Not Found") {
					return nil, nil
				}
				return nil, err
			}
			for _, r := range page.Data {
				playlists = append(playlists, toPlaylist(r))
			}
			endpoint = page.Next
		}
		return playlists, nil
	}

	limit := 100
	playlists := make([]provider.Playlist, total)
	for i, r := range firstPage.Data {
		if i < total {
			playlists[i] = toPlaylist(r)
		}
	}

	if total <= len(firstPage.Data) {
		return playlists[:len(firstPage.Data)], nil
	}

	offsets := []int{}
	for offset := len(firstPage.Data); offset < total; offset += limit {
		offsets = append(offsets, offset)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(offsets))
	sem := make(chan struct{}, 10)

	for _, offset := range offsets {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			u, err := url.Parse(baseEndpoint)
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			q := u.Query()
			q.Set("offset", strconv.Itoa(offset))
			q.Set("limit", strconv.Itoa(limit))
			u.RawQuery = q.Encode()

			req, err := a.newRequest(ctx, http.MethodGet, u.String())
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				return
			}
			var page paginatedPlaylists
			if err := a.do(req, &page); err != nil {
				if strings.Contains(err.Error(), "404 Not Found") {
					return
				}
				select {
				case errChan <- err:
				default:
				}
				return
			}

			for j, r := range page.Data {
				idx := offset + j
				if idx < len(playlists) {
					playlists[idx] = toPlaylist(r)
				}
			}
		}(offset)
	}

	wg.Wait()

	select {
	case err := <-errChan:
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			var filtered []provider.Playlist
			for _, p := range playlists {
				if p.ID != "" {
					filtered = append(filtered, p)
				}
			}
			if len(filtered) > 0 {
				return filtered, nil
			}
		}
		return nil, err
	default:
	}

	var finalPlaylists []provider.Playlist
	for _, p := range playlists {
		if p.ID != "" {
			finalPlaylists = append(finalPlaylists, p)
		}
	}
	return finalPlaylists, nil
}

func (a *AppleProvider) GetLibraryPlaylists(ctx context.Context) ([]provider.Playlist, error) {
	endpoint := "/me/library/playlists?limit=100"
	playlists, err := a.fetchPlaylistsPaginated(ctx, endpoint)
	if err != nil {
		if strings.Contains(err.Error(), "404 Not Found") {
			playlists = nil
		} else {
			return nil, err
		}
	}
	if !hasFavoritesPlaylist(playlists) {
		playlists = append([]provider.Playlist{{ID: favoritesPlaylistID, Name: favoritesPlaylistName, ReadOnly: true}}, playlists...)
	}
	return playlists, nil
}

func hasFavoritesPlaylist(playlists []provider.Playlist) bool {
	for _, pl := range playlists {
		name := strings.ToLower(strings.TrimSpace(pl.Name))
		// Apple localizes the built-in Favorites playlist name. MusicKit library
		// playlist responses do not expose a stable built-in Favorites identifier,
		// so v1 only suppresses the synthetic playlist for known localized names.
		switch name {
		case "favorites", "favorite songs", "brani preferiti", "preferiti", "lieblingstitel", "favoriten", "starred", "starred songs", "favoritas", "favoritos", "canciones favoritas", "músicas favoritas", "morceaux préférés":
			return true
		}
	}
	return false
}

func (a *AppleProvider) GetPlaylistTracks(ctx context.Context, playlistID string) ([]provider.Track, error) {
	if playlistID == favoritesPlaylistID {
		return a.getFavoriteTracks(ctx)
	}

	endpoint := fmt.Sprintf("/me/library/playlists/%s/tracks?limit=100", url.PathEscape(playlistID))
	tracks, err := a.fetchSongsPaginated(ctx, endpoint, false)
	if err != nil {
		if strings.Contains(err.Error(), "404 Not Found") {
			fallbackReq, reqErr := a.newRequest(ctx, http.MethodGet, fmt.Sprintf("/me/library/playlists/%s?include=tracks", url.PathEscape(playlistID)))
			if reqErr != nil {
				return nil, reqErr
			}
			var playlistResp playlistWithTracksResponse
			if doErr := a.do(fallbackReq, &playlistResp); doErr != nil {
				return nil, err
			}
			if len(playlistResp.Data) == 0 {
				return nil, err
			}
			for _, s := range playlistResp.Data[0].Relationships.Tracks.Data {
				tracks = append(tracks, toTrack(s))
			}
			return tracks, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (a *AppleProvider) GetAlbumTracks(ctx context.Context, albumID string) ([]provider.Track, error) {
	sf, err := a.storefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetAlbumTracks: %w", err)
	}
	endpoint := withAudioVariants(fmt.Sprintf("/catalog/%s/albums/%s/tracks?limit=100", sf, url.PathEscape(albumID)))
	return a.fetchSongsPaginated(ctx, endpoint, false)
}

// searchArtistLimit caps the artists a search returns.
const searchArtistLimit = 5

// maxArtistAlbumPages bounds GetArtistAlbums at 2,000 albums, far past any
// real discography, so a "next" link that never ends can't loop forever.
const maxArtistAlbumPages = 20

// GetArtistAlbums pages through the artist's catalog albums. Apple returns
// them in no useful order, so they are sorted newest first here; that needs
// every page, so a failed page fails the whole call rather than returning a
// list that looks complete and isn't.
func (a *AppleProvider) GetArtistAlbums(ctx context.Context, artistID string) ([]provider.Album, error) {
	sf, err := a.storefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetArtistAlbums: %w", err)
	}
	var albums []provider.Album
	endpoint := fmt.Sprintf("/catalog/%s/artists/%s/albums?limit=100", sf, url.PathEscape(artistID))
	for pages := 0; endpoint != ""; pages++ {
		if pages == maxArtistAlbumPages {
			return nil, fmt.Errorf("GetArtistAlbums: more than %d pages", maxArtistAlbumPages)
		}
		req, err := a.newRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var page paginatedAlbums
		if err := a.do(req, &page); err != nil {
			return nil, fmt.Errorf("GetArtistAlbums: %w", err)
		}
		for _, r := range page.Data {
			albums = append(albums, toAlbum(r))
		}
		endpoint = page.Next
	}
	// ISO dates sort as strings; a bare "YYYY" sorts before that year's dates.
	slices.SortStableFunc(albums, func(x, y provider.Album) int { return strings.Compare(y.ReleaseDate, x.ReleaseDate) })
	return albums, nil
}

func (a *AppleProvider) GetLibraryAlbumTracks(ctx context.Context, albumID string) ([]provider.Track, error) {
	endpoint := fmt.Sprintf("/me/library/albums/%s/tracks?limit=100", url.PathEscape(albumID))
	return a.fetchSongsPaginated(ctx, endpoint, false)
}

func (a *AppleProvider) GetCatalogPlaylistTracks(ctx context.Context, playlistID string) ([]provider.Track, error) {
	sf, err := a.storefront(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetCatalogPlaylistTracks: %w", err)
	}
	endpoint := withAudioVariants(fmt.Sprintf("/catalog/%s/playlists/%s/tracks?limit=100", sf, url.PathEscape(playlistID)))
	return a.fetchSongsPaginated(ctx, endpoint, false)
}

func (a *AppleProvider) getFavoriteTracks(ctx context.Context) ([]provider.Track, error) {
	tracks, err := a.GetLibraryTracks(ctx)
	if err != nil {
		return nil, fmt.Errorf("favorites: library tracks: %w", err)
	}
	catalogIDs := make([]string, 0, len(tracks))
	trackCatalogID := make(map[string]string, len(tracks))
	seen := make(map[string]bool, len(tracks))
	for _, track := range tracks {
		catalogID := track.CatalogID
		if catalogID == "" && !strings.HasPrefix(track.ID, "i.") {
			catalogID = track.ID
		}
		if catalogID == "" {
			continue
		}
		trackCatalogID[track.ID] = catalogID
		if !seen[catalogID] {
			catalogIDs = append(catalogIDs, catalogID)
			seen[catalogID] = true
		}
	}
	if len(catalogIDs) == 0 {
		return nil, nil
	}
	loved, err := a.getSongRatings(ctx, catalogIDs)
	if err != nil {
		return nil, fmt.Errorf("favorites: ratings: %w", err)
	}
	favorites := make([]provider.Track, 0, len(tracks))
	for _, track := range tracks {
		if loved[trackCatalogID[track.ID]] {
			favorites = append(favorites, track)
		}
	}
	return favorites, nil
}

type ratingsResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Value int `json:"value"`
		} `json:"attributes"`
	} `json:"data"`
}

func (a *AppleProvider) getSongRatings(ctx context.Context, ids []string) (map[string]bool, error) {
	loved := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return loved, nil
	}

	type batch struct {
		ids []string
	}
	batchCount := (len(ids) + ratingBatchSize - 1) / ratingBatchSize
	jobs := make(chan batch, batchCount)
	errs := make(chan error, 1)
	var mu sync.Mutex
	var wg sync.WaitGroup

	workerCount := min(ratingBatchConcurrency, batchCount)
	for range workerCount {
		wg.Go(func() {
			for job := range jobs {
				ratings, err := a.fetchSongRatingsBatch(ctx, job.ids)
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					continue
				}
				mu.Lock()
				for id := range ratings {
					loved[id] = true
				}
				mu.Unlock()
			}
		})
	}

	for start := 0; start < len(ids); start += ratingBatchSize {
		end := min(start+ratingBatchSize, len(ids))
		select {
		case jobs <- batch{ids: ids[start:end]}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errs:
		return nil, err
	default:
	}
	return loved, nil
}

func (a *AppleProvider) fetchSongRatingsBatch(ctx context.Context, ids []string) (map[string]bool, error) {
	ep := "/me/ratings/songs?ids=" + url.QueryEscape(strings.Join(ids, ","))
	req, err := a.newRequest(ctx, http.MethodGet, ep)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req) //nolint:gosec // G704: URL is constructed from config, not user input
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return map[string]bool{}, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("apple music api %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var ratings ratingsResponse
	if len(body) > 0 {
		if err := json.Unmarshal(body, &ratings); err != nil {
			return nil, err
		}
	}
	loved := make(map[string]bool, len(ratings.Data))
	for _, rating := range ratings.Data {
		if rating.Attributes.Value == 1 {
			// GET /v1/me/ratings/songs?ids=<catalog ids> has been observed to return
			// rating resource IDs prefixed as "r.<catalog id>". Apple's public docs do
			// not document the wrapper prefix, so only strip that exact prefix.
			loved[strings.TrimPrefix(rating.ID, "r.")] = true
		}
	}
	return loved, nil
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

// addPlaylistTracksBody is the JSON body for POST /v1/me/library/playlists/{id}/tracks.
type addPlaylistTracksBody struct {
	Data []createPlaylistTrackRef `json:"data"`
}

func (a *AppleProvider) AddToPlaylist(ctx context.Context, playlistID, trackID string) error {
	typ := "songs"
	if strings.HasPrefix(trackID, "i.") {
		typ = "library-songs"
	}
	body := addPlaylistTracksBody{Data: []createPlaylistTrackRef{{ID: trackID, Type: typ}}}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("AddToPlaylist: marshal: %w", err)
	}
	ep := "/me/library/playlists/" + url.PathEscape(playlistID) + "/tracks"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+ep, bytes.NewReader(raw)) //nolint:gosec // G107: URL is constructed from config, not user input
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	req.Header.Set("Content-Type", "application/json")
	return a.do(req, nil)
}

type stationTracksResponse struct {
	Data []songResource `json:"data"`
}

// stationBatchTarget/stationMaxCalls bound how many upcoming tracks a single
// refill pulls. Apple's next-tracks endpoint returns only ~2 tracks per call,
// so we page a few times (each POST advances the station) to give the queue
// meaningful runway, without issuing an unbounded number of requests.
const (
	stationBatchTarget = 10
	stationMaxCalls    = 6
)

// ResolveCatalogSongID maps a library song ID ("i.XXXX") to its Apple Music
// catalog ID using the /v1/me/library/songs/{id} endpoint.
func (a *AppleProvider) ResolveCatalogSongID(ctx context.Context, libraryID string) (string, error) {
	return a.resolveCatalogSongID(ctx, libraryID)
}

// resolveCatalogSongID maps a library song ID ("i.XXXX") to its Apple Music
// catalog song ID via the song's playParams.catalogId. A station can only be
// seeded by a catalog ID — seeding with a library ID returns HTTP 500. Returns
// an error if the song has no catalog equivalent (e.g. an uploaded/unmatched
// track), for which no per-song station exists.
func (a *AppleProvider) resolveCatalogSongID(ctx context.Context, libraryID string) (string, error) {
	endpoint := "/me/library/songs/" + url.PathEscape(libraryID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)

	var resp stationTracksResponse // same shape: { data: [songResource] }
	if err := a.do(req, &resp); err != nil {
		return "", err
	}
	if len(resp.Data) == 0 || resp.Data[0].Attributes.PlayParams == nil || resp.Data[0].Attributes.PlayParams.CatalogID == "" {
		return "", fmt.Errorf("library song %q has no Apple Music catalog equivalent", libraryID)
	}
	return resp.Data[0].Attributes.PlayParams.CatalogID, nil
}

// stationNextTracksPage fetches one batch (~2 tracks) of upcoming tracks for the
// per-song station ra.<catalogID>. Each POST advances the station. next-tracks
// without an ID only accepts GET; next-tracks/{id} only accepts POST with a body
// (an empty object suffices — the ID carries the seed).
func (a *AppleProvider) stationNextTracksPage(ctx context.Context, catalogID string) ([]provider.Track, error) {
	endpoint := "/me/stations/next-tracks/ra." + url.PathEscape(catalogID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+endpoint, strings.NewReader("{}")) //nolint:gosec // G107: URL is constructed from a resolved catalog ID, not user input
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AppleDeveloperToken)
	req.Header.Set("Music-User-Token", a.cfg.AppleUserToken)
	req.Header.Set("Content-Type", "application/json")

	var resp stationTracksResponse
	if err := a.do(req, &resp); err != nil {
		return nil, err
	}
	tracks := make([]provider.Track, 0, len(resp.Data))
	for _, s := range resp.Data {
		tracks = append(tracks, toTrack(s))
	}
	return tracks, nil
}

// GetStationTracks returns a batch of upcoming tracks for the Apple Music radio
// station seeded by the given song. "ra.<catalogSongID>" is Apple's synthetic
// per-song station ID. The seed MUST be a catalog song ID; a library ID
// ("i.XXXX") is resolved to its catalog ID first (a raw library seed 500s).
// Because each next-tracks call yields only ~2 tracks, several pages are
// accumulated (deduped) up to stationBatchTarget so refills have runway.
func (a *AppleProvider) GetStationTracks(ctx context.Context, seed string) ([]provider.Track, error) {
	if strings.HasPrefix(seed, "i.") {
		catalogID, err := a.resolveCatalogSongID(ctx, seed)
		if err != nil {
			return nil, fmt.Errorf("GetStationTracks: resolve catalog id: %w", err)
		}
		seed = catalogID
	}

	var out []provider.Track
	seen := make(map[string]bool)
	for calls := 0; len(out) < stationBatchTarget && calls < stationMaxCalls; calls++ {
		page, err := a.stationNextTracksPage(ctx, seed)
		if err != nil {
			if len(out) > 0 {
				break // return what we already have rather than fail the refill
			}
			return nil, fmt.Errorf("GetStationTracks: %w", err)
		}
		added := 0
		for _, t := range page {
			key := t.ID
			if key == "" {
				key = t.CatalogID
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
			added++
		}
		if added == 0 {
			break // station returned nothing new — stop paging
		}
	}
	return out, nil
}

// ratingRequest is the body for PUT /v1/me/ratings/songs/{id}.
type ratingRequest struct {
	Type       string           `json:"type"`
	Attributes ratingAttributes `json:"attributes"`
}

type ratingAttributes struct {
	Value int `json:"value"` // 1 = love, -1 = dislike, 0 = neutral
}

// LoveSong adds the song to the user's Apple Music library and marks it as
// Loved (rating value = 1). Pass loved=false to remove the rating. If
// catalogID is a library ID ("i.XXXX"), it resolves the catalog ID first and
// falls back to the library song ratings endpoint if the track has no catalog
// equivalent.
func (a *AppleProvider) LoveSong(ctx context.Context, catalogID string, loved bool) error {
	ratingEndpoint := "/me/ratings/songs/" + url.PathEscape(catalogID)
	if strings.HasPrefix(catalogID, "i.") {
		if resolved, err := a.resolveCatalogSongID(ctx, catalogID); err == nil && resolved != "" {
			catalogID = resolved
			ratingEndpoint = "/me/ratings/songs/" + url.PathEscape(catalogID)
		} else {
			ratingEndpoint = "/me/ratings/library-songs/" + url.PathEscape(catalogID)
		}
	}

	// Add to library first (idempotent — safe to call even if already in library).
	if loved && !strings.HasPrefix(catalogID, "i.") {
		addReq, err := a.newRequest(ctx, http.MethodPost, "/me/library?ids[songs]="+url.QueryEscape(catalogID))
		if err != nil {
			return fmt.Errorf("LoveSong: add to library: %w", err)
		}
		if err := a.do(addReq, nil); err != nil {
			// Non-fatal: song may already be in library.
			_ = err
		}
	}

	if !loved {
		delReq, err := a.newRequest(ctx, http.MethodDelete, ratingEndpoint)
		if err != nil {
			return fmt.Errorf("LoveSong: delete rating: %w", err)
		}
		if err := a.do(delReq, nil); err != nil {
			// Deleting a rating that wasn't set returns 404; treat as success.
			if strings.Contains(err.Error(), "404") {
				return nil
			}
			return err
		}
		return nil
	}

	body := ratingRequest{
		Type:       "ratings",
		Attributes: ratingAttributes{Value: 1},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("LoveSong: marshal: %w", err)
	}
	putReq, err := a.newRequest(ctx, http.MethodPut, ratingEndpoint)
	if err != nil {
		return fmt.Errorf("LoveSong: %w", err)
	}
	putReq.Body = io.NopCloser(bytes.NewReader(raw))
	putReq.ContentLength = int64(len(raw))
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

// GetSongRating returns true when the given song is marked as Loved in the
// user's Apple Music account. If catalogID is a library ID ("i.XXXX"), it
// resolves the catalog ID first and falls back to /me/ratings/library-songs/{id}.
func (a *AppleProvider) GetSongRating(ctx context.Context, catalogID string) (bool, error) {
	ep := "/me/ratings/songs/" + url.PathEscape(catalogID)
	if strings.HasPrefix(catalogID, "i.") {
		if resolved, err := a.resolveCatalogSongID(ctx, catalogID); err == nil && resolved != "" {
			catalogID = resolved
			ep = "/me/ratings/songs/" + url.PathEscape(catalogID)
		} else {
			ep = "/me/ratings/library-songs/" + url.PathEscape(catalogID)
		}
	}
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
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
		title := provider.Clean(r.Attributes.Title.StringForDisplay)
		if title == "" {
			continue
		}
		var items []provider.RecommendationItem
		for _, c := range r.Relationships.Contents.Data {
			item := provider.RecommendationItem{ID: c.ID, Title: provider.Clean(c.Attributes.Name)}
			switch c.Type {
			case "albums":
				item.Kind = "album"
				item.Subtitle = provider.Clean(c.Attributes.ArtistName)
			case "playlists":
				item.Kind = "playlist"
				item.Subtitle = provider.Clean(c.Attributes.CuratorName)
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

// GetCatalogTracks fetches track metadata for a list of catalog song IDs.
func (a *AppleProvider) GetCatalogTracks(ctx context.Context, ids []string) ([]provider.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	sf, err := a.storefront(ctx)
	if err != nil {
		return nil, err
	}
	ep := withAudioVariants(fmt.Sprintf("/catalog/%s/songs?ids=%s", sf, url.QueryEscape(strings.Join(ids, ","))))
	var resp struct {
		Data []songResource `json:"data"`
	}
	if _, err := a.getExtended(ctx, a.newRequest, ep, &resp); err != nil {
		return nil, err
	}
	tracks := make([]provider.Track, len(resp.Data))
	for i, s := range resp.Data {
		tracks[i] = toTrack(s)
	}
	return tracks, nil
}
