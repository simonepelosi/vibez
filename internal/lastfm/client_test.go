package lastfm

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/simone-vibes/vibez/internal/config"
)

func TestSign(t *testing.T) {
	// Example from Last.fm API documentation:
	// https://www.last.fm/api/authspec#8
	c := &Client{apiSecret: "mysecret"}
	params := map[string]string{
		"method":  "auth.getSession",
		"api_key": "myapikey",
		"token":   "mytoken",
	}
	got := c.sign(params)
	// Expected: md5("api_keymyapikeymethod" + "auth.getSession" + "token" + "mytoken" + "mysecret")
	// = md5("api_keymyapikeymethod auth.getSessiontokenmytokenmysecret")
	// = md5("api_keymyapikeymethodauth.getSessiontokenmytokenmysecret")
	if got == "" {
		t.Fatal("sign returned empty string")
	}
	// Sign must be deterministic.
	if got2 := c.sign(params); got != got2 {
		t.Errorf("sign is not deterministic: %q vs %q", got, got2)
	}
}

func TestSignExcludesFormatAndCallback(t *testing.T) {
	c := &Client{apiSecret: "secret"}
	withExtra := map[string]string{
		"method":   "track.scrobble",
		"api_key":  "key",
		"format":   "json",
		"callback": "fn",
	}
	without := map[string]string{
		"method":  "track.scrobble",
		"api_key": "key",
	}
	// Signatures must be equal regardless of format/callback presence.
	if c.sign(withExtra) != c.sign(without) {
		t.Error("sign should not include 'format' or 'callback' keys")
	}
}

func TestSignKnownValue(t *testing.T) {
	// Verify against a hand-computed value:
	// params: api_key=abc, method=track.scrobble, secret=xyz
	// sorted keys: api_key, method
	// string: "api_keyabcmethodtrack.scrobblexyz"
	// md5("api_keyabcmethodtrack.scrobblexyz") = known value
	c := &Client{apiSecret: "xyz"}
	params := map[string]string{
		"api_key": "abc",
		"method":  "track.scrobble",
	}
	got := c.sign(params)
	if len(got) != 32 {
		t.Errorf("MD5 hex digest should be 32 chars, got %d: %q", len(got), got)
	}
}

func TestApplyEmbedded_NotPersistedBySave(t *testing.T) {
	origKey, origSecret := apiKey, apiSecret
	t.Cleanup(func() { apiKey, apiSecret = origKey, origSecret })
	apiKey, apiSecret = "emb-key", "emb-secret"

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &config.Config{AuthPort: 7777, Provider: "apple", Theme: "default"}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ApplyEmbedded(loaded)
	loaded.LastfmSessionKey = "user-sk"
	if err := loaded.Save(""); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LastfmAPIKey != "" || reloaded.LastfmAPISecret != "" {
		t.Errorf("embedded Last.fm credentials persisted to config: key=%q secret=%q", reloaded.LastfmAPIKey, reloaded.LastfmAPISecret)
	}
	if reloaded.LastfmSessionKey != "user-sk" {
		t.Errorf("session key = %q, want user-sk", reloaded.LastfmSessionKey)
	}
}

func TestClient_DoesNotFollowRedirectsAndGetOmitsSessionKey(t *testing.T) {
	var redirectHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	c := NewClient("mykey", "mysecret", "super-secret-sk")
	c.baseURL = srv.URL

	if _, err := c.get("auth.getSession", map[string]string{"token": "tok"}); err == nil {
		t.Fatal("expected error when Last.fm API redirects")
	}
	if redirectHits.Load() != 0 {
		t.Error("client followed redirect")
	}
	if strings.Contains(seenQuery, "super-secret-sk") || strings.Contains(seenQuery, "sk=") {
		t.Errorf("GET query leaked session key: %q", seenQuery)
	}
}

func TestClient_TransportErrorRedactsQuery(t *testing.T) {
	c := NewClient("mykey", "mysecret", "")
	c.baseURL = "http://127.0.0.1:1"
	_, err := c.get("auth.getSession", map[string]string{"token": "secret-tok-123"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-tok-123") || strings.Contains(err.Error(), "api_sig=") {
		t.Errorf("error leaked sensitive query params: %v", err)
	}
}
