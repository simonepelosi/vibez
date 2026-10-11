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
	"sync"
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
	// maxFetchAttempts is the maximum number of attempts for broker requests.
	maxFetchAttempts = 3
)

var (
	retryBackoff   = 300 * time.Millisecond //nolint:gochecknoglobals // modified in tests
	brokerLogsMu   sync.Mutex               //nolint:gochecknoglobals
	lastBrokerLogs []string                 //nolint:gochecknoglobals
)

func logBroker(msg string) {
	brokerLogsMu.Lock()
	defer brokerLogsMu.Unlock()
	lastBrokerLogs = append(lastBrokerLogs, msg)
}

func resetBrokerLogs() {
	brokerLogsMu.Lock()
	defer brokerLogsMu.Unlock()
	lastBrokerLogs = nil
}

// BrokerLogs returns a copy of the log messages produced during the most
// recent developer token resolution.
func BrokerLogs() []string {
	brokerLogsMu.Lock()
	defer brokerLogsMu.Unlock()
	out := make([]string, len(lastBrokerLogs))
	copy(out, lastBrokerLogs)
	return out
}

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
	resetBrokerLogs()
	if devToken != "" {
		logBroker("[broker] using build-time embedded developer token")
		cfg.AppleDeveloperToken = devToken
		return nil
	}
	if tokenURL == "" {
		if cfg.AppleDeveloperToken != "" {
			logBroker("[broker] using developer token from config file")
		}
		return nil
	}
	tok, err := brokerToken(tokenURL, clientKey, cachePath(), time.Now())
	if err != nil {
		if cfg.AppleDeveloperToken != "" {
			logBroker(fmt.Sprintf("[broker] falling back to configured apple_developer_token after broker error: %v", err))
		}
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

type nonRetryableError struct{ error }

func (e nonRetryableError) Unwrap() error { return e.error }

func isPermanentError(err error) bool {
	var nonRetryable nonRetryableError
	return errors.As(err, &nonRetryable)
}

// brokerToken returns a developer token from the on-disk cache when it has
// time left, otherwise from the broker. If the broker is unreachable it falls
// back to a cached token that has not yet expired.
func brokerToken(endpoint, key, cache string, now time.Time) (string, error) {
	cached, _ := readCache(cache)
	if cached.validFor(now, refreshBefore) {
		remaining := time.Until(time.Unix(cached.ExpiresAt, 0)).Round(time.Minute)
		logBroker(fmt.Sprintf("[broker] using cached developer token (valid for %v)", remaining))
		return cached.Token, nil
	}

	if cached.validFor(now, 0) {
		remaining := time.Until(time.Unix(cached.ExpiresAt, 0)).Round(time.Minute)
		logBroker(fmt.Sprintf("[broker] cached token expiring soon (%v remaining), fetching fresh token from %s", remaining, endpoint))
	} else {
		logBroker(fmt.Sprintf("[broker] fetching fresh developer token from %s", endpoint))
	}

	fetched, err := fetchTokenWithRetry(endpoint, key, now)
	if err == nil {
		validity := time.Until(time.Unix(fetched.ExpiresAt, 0)).Round(time.Minute)
		logBroker(fmt.Sprintf("[broker] fresh developer token received (valid for %v)", validity))
		_ = writeCache(cache, fetched)
		return fetched.Token, nil
	}

	if cached.validFor(now, 0) {
		remaining := time.Until(time.Unix(cached.ExpiresAt, 0)).Round(time.Minute)
		logBroker(fmt.Sprintf("[broker] all fetch attempts failed, falling back to unexpired cached token (valid for %v)", remaining))
		return cached.Token, nil
	}

	logBroker(fmt.Sprintf("[broker] all fetch attempts failed and no valid cache available: %v", err))
	return "", fmt.Errorf("fetching developer token from broker: %w", err)
}

func fetchTokenWithRetry(endpoint, key string, now time.Time) (cachedToken, error) {
	var lastErr error
	backoff := retryBackoff

	for attempt := 1; attempt <= maxFetchAttempts; attempt++ {
		tok, err := fetchToken(endpoint, key, now)
		if err == nil {
			return tok, nil
		}
		lastErr = err
		logBroker(fmt.Sprintf("[broker] attempt %d/%d failed: %v", attempt, maxFetchAttempts, err))

		if isPermanentError(err) || attempt == maxFetchAttempts {
			break
		}

		logBroker(fmt.Sprintf("[broker] retrying in %v...", backoff))
		time.Sleep(backoff)
		backoff *= 2
	}
	return cachedToken{}, lastErr
}

// fetchToken asks the broker for a token. The endpoint must be HTTPS, or HTTP
// on a loopback host so tests can use httptest. Redirects are refused: the
// client key must not follow a redirect to another host.
func fetchToken(endpoint, key string, now time.Time) (cachedToken, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return cachedToken{}, nonRetryableError{fmt.Errorf("token url: %w", err)}
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !isLoopback(u.Hostname())) {
		return cachedToken{}, nonRetryableError{errors.New("token url must be https")}
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

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return cachedToken{}, nonRetryableError{fmt.Errorf("token broker returned %s", resp.Status)}
	}
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
		return cachedToken{}, nonRetryableError{errors.New("token broker returned a malformed token")}
	}
	if !time.Unix(out.ExpiresAt, 0).After(now) {
		return cachedToken{}, nonRetryableError{errors.New("token broker returned an expired token")}
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
