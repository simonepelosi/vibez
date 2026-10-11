package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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
	srv := brokerServer(t, "k", fakeJWT, now.Add(24*time.Hour), &hits)
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
	srv := brokerServer(t, "k", fakeJWT, now.Add(24*time.Hour), &hits)
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
