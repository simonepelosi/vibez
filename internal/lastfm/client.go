package lastfm

import (
	"crypto/md5" //nolint:gosec // Last.fm API requires MD5 for signatures; not used for security
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	apiBaseURL = "https://ws.audioscrobbler.com/2.0/"
	authURL    = "https://www.last.fm/api/auth/"
)

// Client is a Last.fm API client. Create with NewClient.
type Client struct {
	apiKey     string
	apiSecret  string
	sessionKey string
	http       *http.Client
}

// NewClient returns a Last.fm API client. sessionKey may be empty for
// unauthenticated calls such as auth.getToken and auth.getSession.
func NewClient(apiKey, apiSecret, sessionKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		sessionKey: sessionKey,
		http:       &http.Client{Timeout: 15 * time.Second},
	}
}

// sign computes the Last.fm API signature for params.
// "format" and "callback" are excluded from the signature per the Last.fm spec.
func (c *Client) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "format" || k == "callback" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(params[k])
	}
	sb.WriteString(c.apiSecret)

	//nolint:gosec // MD5 required by Last.fm API specification
	h := md5.Sum([]byte(sb.String()))
	return fmt.Sprintf("%x", h)
}

// post makes a signed POST request to the Last.fm API and returns the body.
// It adds api_key, sk (if set), api_sig, and format=json automatically.
func (c *Client) post(method string, params map[string]string) ([]byte, error) {
	params["method"] = method
	params["api_key"] = c.apiKey
	if c.sessionKey != "" {
		params["sk"] = c.sessionKey
	}
	params["api_sig"] = c.sign(params)
	params["format"] = "json"

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	resp, err := c.http.PostForm(apiBaseURL, form) //nolint:noctx // short-lived helper; Client has a timeout
	if err != nil {
		return nil, fmt.Errorf("last.fm request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading last.fm response: %w", err)
	}

	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	return body, nil
}

// get makes a signed GET request to the Last.fm API and returns the body.
// Used for auth.getToken and auth.getSession which are typically called via GET.
func (c *Client) get(method string, params map[string]string) ([]byte, error) {
	params["method"] = method
	params["api_key"] = c.apiKey
	if c.sessionKey != "" {
		params["sk"] = c.sessionKey
	}
	params["api_sig"] = c.sign(params)
	params["format"] = "json"

	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}

	resp, err := c.http.Get(apiBaseURL + "?" + q.Encode()) //nolint:noctx // short-lived helper; Client has a timeout
	if err != nil {
		return nil, fmt.Errorf("last.fm request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading last.fm response: %w", err)
	}

	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	return body, nil
}

func checkAPIError(body []byte) error {
	var apiErr struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != 0 {
		return fmt.Errorf("last.fm API error %d: %s", apiErr.Error, apiErr.Message)
	}
	return nil
}

// ── Authentication ────────────────────────────────────────────────────────

// GetToken requests a temporary auth token from Last.fm (auth.getToken).
// The user must visit AuthorizeURL(token) and grant access before GetSession.
func (c *Client) GetToken() (string, error) {
	body, err := c.get("auth.getToken", map[string]string{})
	if err != nil {
		return "", err
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing last.fm token response: %w", err)
	}
	if resp.Token == "" {
		return "", fmt.Errorf("last.fm returned an empty token")
	}
	return resp.Token, nil
}

// AuthorizeURL returns the Last.fm URL the user must visit to grant access.
func (c *Client) AuthorizeURL(token string) string {
	return fmt.Sprintf("%s?api_key=%s&token=%s", authURL, c.apiKey, token)
}

// GetSession exchanges an authorized token for a session key (auth.getSession).
// Call this after the user has visited AuthorizeURL and granted access.
func (c *Client) GetSession(token string) (string, error) {
	body, err := c.get("auth.getSession", map[string]string{"token": token})
	if err != nil {
		return "", err
	}
	var resp struct {
		Session struct {
			Key string `json:"key"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing last.fm session response: %w", err)
	}
	if resp.Session.Key == "" {
		return "", fmt.Errorf("last.fm returned an empty session key")
	}
	return resp.Session.Key, nil
}

// ── Scrobbling ────────────────────────────────────────────────────────────

// UpdateNowPlaying notifies Last.fm that the user is currently listening to a
// track (track.updateNowPlaying). Requires a valid session key.
func (c *Client) UpdateNowPlaying(artist, track, album string, duration time.Duration) error {
	params := map[string]string{
		"artist": artist,
		"track":  track,
	}
	if album != "" {
		params["album"] = album
	}
	if duration > 0 {
		params["duration"] = fmt.Sprintf("%d", int(duration.Seconds()))
	}
	_, err := c.post("track.updateNowPlaying", params)
	return err
}

// Scrobble submits a track play to Last.fm (track.scrobble).
// startTime is the Unix timestamp of when playback began.
// Requires a valid session key.
func (c *Client) Scrobble(artist, track, album string, startTime time.Time, duration time.Duration) error {
	params := map[string]string{
		"artist":    artist,
		"track":     track,
		"timestamp": fmt.Sprintf("%d", startTime.Unix()),
	}
	if album != "" {
		params["album"] = album
	}
	if duration > 0 {
		params["duration"] = fmt.Sprintf("%d", int(duration.Seconds()))
	}
	_, err := c.post("track.scrobble", params)
	return err
}
