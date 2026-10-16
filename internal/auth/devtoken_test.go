package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
)

const fakeJWT = "aaa.bbb.ccc" //nolint:gosec // G101: test value, not a real credential

func brokerServer(t *testing.T, key, token string, exp time.Time, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("X-Vibez-Client") != key {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + token + `","expires_at":` + itoa(exp.Unix()) + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestBrokerToken_FetchesAndCaches(t *testing.T) {
	now := time.Now()
	var hits atomic.Int32
	srv := brokerServer(t, "k", fakeJWT, now.Add(7*24*time.Hour), &hits)
	cache := filepath.Join(t.TempDir(), "sub", "devtoken.json")

	got, err := brokerToken(srv.URL, "k", cache, now)
	if err != nil || got != fakeJWT {
		t.Fatalf("brokerToken = %q, %v", got, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	// Second call is served from the cache.
	got, err = brokerToken(srv.URL, "k", cache, now.Add(time.Hour))
	if err != nil || got != fakeJWT {
		t.Fatalf("cached brokerToken = %q, %v", got, err)
	}
	if hits.Load() != 1 {
		t.Errorf("cache miss: hits = %d, want 1", hits.Load())
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stat(cache)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("cache mode = %v, want 0600", fi.Mode().Perm())
		}
	}
}

func TestBrokerToken_RefetchesNearExpiry(t *testing.T) {
	now := time.Now()
	var hits atomic.Int32
	srv := brokerServer(t, "k", fakeJWT, now.Add(7*24*time.Hour), &hits)
	cache := filepath.Join(t.TempDir(), "devtoken.json")
	if err := writeCache(cache, cachedToken{Token: "old.old.old", ExpiresAt: now.Add(refreshBefore / 2).Unix()}); err != nil {
		t.Fatal(err)
	}

	got, err := brokerToken(srv.URL, "k", cache, now)
	if err != nil || got != fakeJWT {
		t.Fatalf("brokerToken = %q, %v; want a fresh token", got, err)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
}

func TestBrokerToken_FallsBackToUnexpiredCacheWhenBrokerFails(t *testing.T) {
	now := time.Now()
	var hits atomic.Int32
	srv := brokerServer(t, "right", fakeJWT, now.Add(time.Hour), &hits) // wrong key -> 401
	cache := filepath.Join(t.TempDir(), "devtoken.json")
	if err := writeCache(cache, cachedToken{Token: "old.old.old", ExpiresAt: now.Add(refreshBefore / 2).Unix()}); err != nil {
		t.Fatal(err)
	}

	got, err := brokerToken(srv.URL, "wrong", cache, now)
	if err != nil || got != "old.old.old" {
		t.Fatalf("brokerToken = %q, %v; want the still-valid cached token", got, err)
	}
}

func TestBrokerToken_FailsWithoutUsableCache(t *testing.T) {
	now := time.Now()
	var hits atomic.Int32
	srv := brokerServer(t, "right", fakeJWT, now.Add(time.Hour), &hits)
	cache := filepath.Join(t.TempDir(), "devtoken.json")
	if err := writeCache(cache, cachedToken{Token: "old.old.old", ExpiresAt: now.Add(-time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}

	if got, err := brokerToken(srv.URL, "wrong", cache, now); err == nil {
		t.Fatalf("brokerToken = %q, nil; want an error with only an expired cache", got)
	}
}

func TestFetchToken_RejectsBadResponses(t *testing.T) {
	now := time.Now()
	cases := map[string]string{
		"malformed token": `{"token":"not-a-jwt","expires_at":` + itoa(now.Add(time.Hour).Unix()) + `}`,
		"empty segment":   `{"token":"a..c","expires_at":` + itoa(now.Add(time.Hour).Unix()) + `}`,
		"js injection":    `{"token":"a.b.';alert(1)//","expires_at":` + itoa(now.Add(time.Hour).Unix()) + `}`,
		"expired token":   `{"token":"` + fakeJWT + `","expires_at":` + itoa(now.Add(-time.Hour).Unix()) + `}`,
		"not json":        `<html>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			if _, err := fetchToken(srv.URL, "k", now); err == nil {
				t.Errorf("fetchToken accepted %s", name)
			}
		})
	}
}

func TestFetchToken_RefusesPlainHTTPToRemoteHost(t *testing.T) {
	if _, err := fetchToken("http://example.com/v1/token", "k", time.Now()); err == nil {
		t.Error("fetchToken accepted http to a non-loopback host")
	}
}

func TestFetchToken_DoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked.Add(1) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()

	if _, err := fetchToken(srv.URL, "secret", time.Now()); err == nil {
		t.Error("fetchToken followed a redirect")
	}
	if leaked.Load() != 0 {
		t.Error("the client key reached the redirect target")
	}
}

func TestResolveDeveloperToken(t *testing.T) {
	origDev, origURL, origKey := devToken, tokenURL, clientKey
	t.Cleanup(func() { devToken, tokenURL, clientKey = origDev, origURL, origKey })

	t.Run("embedded token wins and needs no network", func(t *testing.T) {
		devToken, tokenURL, clientKey = "embedded.tok.en", "http://127.0.0.1:1/unused", "" //nolint:gosec // G101: test value, not a real credential
		cfg := &config.Config{AppleDeveloperToken: "stale"}
		if err := ResolveDeveloperToken(cfg); err != nil || cfg.AppleDeveloperToken != "embedded.tok.en" {
			t.Errorf("got %q, %v", cfg.AppleDeveloperToken, err)
		}
	})

	t.Run("neither configured leaves the config value", func(t *testing.T) {
		devToken, tokenURL, clientKey = "", "", ""
		cfg := &config.Config{AppleDeveloperToken: "mine"}
		if err := ResolveDeveloperToken(cfg); err != nil || cfg.AppleDeveloperToken != "mine" {
			t.Errorf("got %q, %v", cfg.AppleDeveloperToken, err)
		}
	})

	t.Run("broker failure keeps the config token and reports the error", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		t.Setenv("HOME", t.TempDir())
		t.Setenv("LOCALAPPDATA", t.TempDir())
		var hits atomic.Int32
		srv := brokerServer(t, "right", fakeJWT, time.Now().Add(time.Hour), &hits)
		devToken, tokenURL, clientKey = "", srv.URL, "wrong"
		cfg := &config.Config{AppleDeveloperToken: "mine"}
		if err := ResolveDeveloperToken(cfg); err == nil {
			t.Error("want an error when the broker rejects the client")
		}
		if cfg.AppleDeveloperToken != "mine" {
			t.Errorf("config token clobbered: %q", cfg.AppleDeveloperToken)
		}
	})
}

func TestBrokerToken_RetriesOnTransientErrors(t *testing.T) {
	origBackoff := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = origBackoff })

	var hits atomic.Int32
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := hits.Add(1)
		if count == 1 {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + fakeJWT + `","expires_at":` + itoa(now.Add(7*24*time.Hour).Unix()) + `}`))
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "devtoken.json")
	tok, err := brokerToken(srv.URL, "k", cache, now)
	if err != nil {
		t.Fatalf("brokerToken failed: %v", err)
	}
	if tok != fakeJWT {
		t.Errorf("got token %q, want %q", tok, fakeJWT)
	}
	if hits.Load() != 2 {
		t.Errorf("hits = %d, want 2", hits.Load())
	}

	logs := BrokerLogs()
	foundAttempt1 := false
	foundRetry := false
	foundSuccess := false
	for _, l := range logs {
		if strings.Contains(l, "attempt 1/3 failed") {
			foundAttempt1 = true
		}
		if strings.Contains(l, "retrying in") {
			foundRetry = true
		}
		if strings.Contains(l, "fresh developer token received") {
			foundSuccess = true
		}
	}
	if !foundAttempt1 || !foundRetry || !foundSuccess {
		t.Errorf("missing expected log messages in %v", logs)
	}
}

func TestBrokerToken_DoesNotRetryPermanentErrors(t *testing.T) {
	origBackoff := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = origBackoff })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "devtoken.json")
	_, err := brokerToken(srv.URL, "bad-key", cache, time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1 (should not retry 401)", hits.Load())
	}
}

func TestBrokerToken_SingleAttemptWhenFallingBackToValidCache(t *testing.T) {
	origBackoff := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = origBackoff })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	const fallbackJWT = "fallback.jwt.val" //nolint:gosec // G101: test value
	now := time.Now()
	cache := filepath.Join(t.TempDir(), "devtoken.json")
	// Cache is expiring soon (within refreshBefore) but not yet expired
	if err := writeCache(cache, cachedToken{Token: fallbackJWT, ExpiresAt: now.Add(30 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}

	tok, err := brokerToken(srv.URL, "k", cache, now)
	if err != nil {
		t.Fatalf("expected fallback to cache, got err: %v", err)
	}
	if tok != fallbackJWT {
		t.Errorf("got %q, want cached token", tok)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1 (should not multi-retry when a valid cache exists)", hits.Load())
	}

	logs := BrokerLogs()
	foundFallback := false
	for _, l := range logs {
		if strings.Contains(l, "fetch failed, falling back to unexpired cached token") {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Errorf("missing fallback log in %v", logs)
	}
}
