package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
)

// devToken is the Apple Music developer JWT injected at build time via -ldflags:
//
//	-X 'github.com/simone-vibes/vibez/internal/auth.devToken=<jwt>'
//
// When empty the user must set apple_developer_token in their config file.
// garble -literals obfuscates this string so it does not appear in plaintext.
var devToken string //nolint:gochecknoglobals // intentionally set via ldflags

// tokenURL and clientKey are injected at build time via -ldflags by release
// builds, which fetch a short-lived developer token from the vibez token
// broker instead of embedding one:
//
//	-X 'github.com/simone-vibes/vibez/internal/auth.tokenURL=https://…/v1/token'
//	-X 'github.com/simone-vibes/vibez/internal/auth.clientKey=<shared header value>'
//
// The token is not secret from whoever runs vibez (MusicKit JS needs it in the
// page); fetching it keeps the signing key off the binary, lets a leaked token
// expire within a day and lets the key rotate without a release.
var (
	tokenURL  string //nolint:gochecknoglobals // intentionally set via ldflags
	clientKey string //nolint:gochecknoglobals // intentionally set via ldflags
)

const (
	// refreshBefore is how close to expiry a cached token stops being reused.
	refreshBefore = 2 * time.Hour
	// fetchTimeout bounds the startup round trip.
	fetchTimeout = 5 * time.Second
	// maxTokenResponse bounds the broker response body.
	maxTokenResponse = 8 << 10
)

// ApplyEmbedded sets cfg.AppleDeveloperToken from the build-time embedded
// token, or from the token broker when the build points at one. The result
// always takes priority over any value in the config file, ensuring release
// binaries use the canonical token rather than a stale one.
//
// Failures are not reported here; callers that need the reason use
// ResolveDeveloperToken.
func ApplyEmbedded(cfg *config.Config) {
	_ = ResolveDeveloperToken(cfg)
}

// ResolveDeveloperToken is ApplyEmbedded with the failure reason. A broker
// error is returned only when no usable token was found; a fetch that failed
// but left a still-valid cached token succeeds.
func ResolveDeveloperToken(cfg *config.Config) error {
	if devToken != "" {
		cfg.AppleDeveloperToken = devToken
		return nil
	}
	if tokenURL == "" {
		return nil
	}
	tok, err := brokerToken(tokenURL, clientKey, cachePath(), time.Now())
	if err != nil {
		return err
	}
	cfg.AppleDeveloperToken = tok
	return nil
}

type cachedToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

func (c cachedToken) validFor(now time.Time, d time.Duration) bool {
	return c.Token != "" && time.Unix(c.ExpiresAt, 0).After(now.Add(d))
}

// brokerToken returns a developer token from the on-disk cache when it has
// time left, otherwise from the broker. If the broker is unreachable it falls
// back to a cached token that has not yet expired.
func brokerToken(endpoint, key, cache string, now time.Time) (string, error) {
	cached, _ := readCache(cache)
	if cached.validFor(now, refreshBefore) {
		return cached.Token, nil
	}

	fetched, err := fetchToken(endpoint, key, now)
	if err != nil {
		if cached.validFor(now, 0) {
			return cached.Token, nil
		}
		return "", fmt.Errorf("fetching developer token: %w", err)
	}
	_ = writeCache(cache, fetched) // best effort: a cache miss only costs a round trip
	return fetched.Token, nil
}

// fetchToken asks the broker for a token. The endpoint must be HTTPS, or HTTP
// on a loopback host so tests can use httptest. Redirects are refused: the
// client key must not follow a redirect to another host.
func fetchToken(endpoint, key string, now time.Time) (cachedToken, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return cachedToken{}, fmt.Errorf("token url: %w", err)
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname())) {
		return cachedToken{}, errors.New("token url must be https")
	}

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return cachedToken{}, err
	}
	req.Header.Set("X-Vibez-Client", key)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		Timeout:       fetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return cachedToken{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return cachedToken{}, fmt.Errorf("token broker returned %s", resp.Status)
	}

	var out struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTokenResponse)).Decode(&out); err != nil {
		return cachedToken{}, fmt.Errorf("token broker response: %w", err)
	}
	if strings.Count(out.Token, ".") != 2 {
		return cachedToken{}, errors.New("token broker returned a malformed token")
	}
	if !time.Unix(out.ExpiresAt, 0).After(now) {
		return cachedToken{}, errors.New("token broker returned an expired token")
	}
	return cachedToken{Token: out.Token, ExpiresAt: out.ExpiresAt}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// cachePath is where the fetched token is kept between runs, or "" when the
// platform has no user cache directory.
func cachePath() string {
	d, err := os.UserCacheDir()
	if err != nil || d == "" {
		return ""
	}
	return filepath.Join(d, "vibez", "devtoken.json")
}

func readCache(path string) (cachedToken, error) {
	var c cachedToken
	if path == "" {
		return c, errors.New("no cache dir")
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: fixed path under the user cache dir
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func writeCache(path string, c cachedToken) error {
	if path == "" {
		return errors.New("no cache dir")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
