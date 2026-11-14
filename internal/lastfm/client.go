package lastfm

import (
	"context"
	"crypto/md5" //nolint:gosec // Last.fm API requires MD5 for signatures; not used for security
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	apiBaseURL = "https://ws.audioscrobbler.com/2.0/"
	authURL    = "https://www.last.fm/api/auth/"

	// maxResponseBody bounds how much of any API response is read.
	maxResponseBody = 4 << 20
	requestTimeout  = 15 * time.Second
)

// Client is a Last.fm API client. Create with NewClient.
type Client struct {
	apiKey     string
	apiSecret  string
	sessionKey string
	baseURL    string
	http       *http.Client
}

// NewClient returns a Last.fm API client. sessionKey may be empty for
// unauthenticated calls such as auth.getToken and auth.getSession.
func NewClient(apiKey, apiSecret, sessionKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		sessionKey: sessionKey,
		http: &http.Client{
			Timeout: requestTimeout,
			// Never follow redirects when sending signed API parameters or session keys.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) apiEndpoint() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return apiBaseURL
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
	form := c.signedValues(method, params, true)
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiEndpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building last.fm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.doRequest(req)
}

// get makes a signed GET request to the Last.fm API and returns the body.
// Used for unauthenticated auth.getToken and auth.getSession; never puts sk in URLs.
func (c *Client) get(method string, params map[string]string) ([]byte, error) {
	q := c.signedValues(method, params, false)
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiEndpoint()+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building last.fm request: %w", err)
	}
	return c.doRequest(req)
}

func (c *Client) signedValues(method string, params map[string]string, includeSession bool) url.Values {
	signed := make(map[string]string, len(params)+4)
	maps.Copy(signed, params)
	signed["method"] = method
	signed["api_key"] = c.apiKey
	if includeSession && c.sessionKey != "" {
		signed["sk"] = c.sessionKey
	}
	signed["api_sig"] = c.sign(signed)
	signed["format"] = "json"

	vals := url.Values{}
	for k, v := range signed {
		vals.Set(k, v)
	}
	return vals
}

func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req) //nolint:gosec // G704: request URL targets fixed apiBaseURL (or loopback test override)
	if err != nil {
		return nil, fmt.Errorf("last.fm request: %w", stripURLQuery(err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("last.fm request: unexpected redirect status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading last.fm response: %w", err)
	}

	if err := checkAPIError(body); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("last.fm request failed: HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func stripURLQuery(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			u.RawQuery = ""
			u.Fragment = ""
			u.User = nil
			return &url.Error{Op: ue.Op, URL: u.String(), Err: ue.Err}
		}
		return &url.Error{Op: ue.Op, URL: "<redacted>", Err: ue.Err}
	}
	return err
}

func checkAPIError(body []byte) error {
	var apiErr struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != 0 {
		return fmt.Errorf("last.fm API error %d: %s", apiErr.Error, sanitizeControl(apiErr.Message))
	}
	return nil
}

func sanitizeControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
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
	q := url.Values{"api_key": {c.apiKey}, "token": {token}}
	return authURL + "?" + q.Encode()
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
