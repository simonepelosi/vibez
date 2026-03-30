package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
)

// minimalTemplate is a no-op login page template used in handler tests.
const minimalTemplate = `<html><body>{{.DeveloperToken}}</body></html>`

func newTestMux(t *testing.T) (*http.ServeMux, chan string, chan error) {
	t.Helper()
	tmpl, err := template.New("login").Parse(minimalTemplate)
	if err != nil {
		t.Fatalf("parsing test template: %v", err)
	}
	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := buildMux("TEST_DEV_TOKEN", tmpl, tokenCh, errCh)
	return mux, tokenCh, errCh
}

// --- Login error cases ---

func TestLogin_MissingDeveloperToken(t *testing.T) {
	cfg := &config.Config{
		AppleDeveloperToken: "",
		AuthPort:            17777,
	}
	err := Login(cfg)
	if err == nil {
		t.Fatal("expected error when developer token is missing, got nil")
	}
}

// --- /login handler ---

func TestLoginHandler_ServesHTML(t *testing.T) {
	mux, _, _ := newTestMux(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type header missing")
	}
	body := w.Body.String()
	if body == "" {
		t.Error("response body is empty")
	}
	// Developer token must be injected into the page.
	if !containsSubstr(body, "TEST_DEV_TOKEN") {
		t.Errorf("developer token not found in response body: %s", body)
	}
}

// --- /callback handler ---

func TestCallbackHandler_AcceptsValidToken(t *testing.T) {
	mux, tokenCh, _ := newTestMux(t)

	body, _ := json.Marshal(callbackPayload{UserToken: "valid-user-token"})
	req := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	select {
	case got := <-tokenCh:
		if got != "valid-user-token" {
			t.Errorf("token = %q, want %q", got, "valid-user-token")
		}
	default:
		t.Error("no token sent to channel")
	}
}

func TestCallbackHandler_RejectsGET(t *testing.T) {
	mux, _, _ := newTestMux(t)
	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestCallbackHandler_RejectsBadJSON(t *testing.T) {
	mux, _, errCh := newTestMux(t)
	req := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewBufferString("{not-json"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected non-nil error on errCh")
		}
	default:
		t.Error("no error sent to errCh")
	}
}

func TestCallbackHandler_RejectsEmptyToken(t *testing.T) {
	mux, _, errCh := newTestMux(t)
	body, _ := json.Marshal(callbackPayload{UserToken: ""})
	req := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected non-nil error on errCh for empty token")
		}
	default:
		t.Error("no error sent to errCh")
	}
}

// --- Logout ---

func TestLogout_ClearsUserToken(t *testing.T) {
	path := t.TempDir() + "/config.json"
	cfg := &config.Config{
		AppleUserToken: "token-to-remove",
		StoreFront:     "us",
		AuthPort:       7777,
		Provider:       "apple",
		Theme:          "default",
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	if err := Logout(cfg); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if cfg.AppleUserToken != "" {
		t.Errorf("in-memory token not cleared: %q", cfg.AppleUserToken)
	}
}

func TestLogout_PersistsEmptyToken(t *testing.T) {
	path := t.TempDir() + "/config.json"
	cfg := &config.Config{
		AppleUserToken: "persist-me-cleared",
		StoreFront:     "us",
		AuthPort:       7777,
		Provider:       "apple",
		Theme:          "default",
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	// Override the save path by calling Save manually after Logout.
	if err := Logout(cfg); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.AppleUserToken != "" {
		t.Errorf("persisted token not cleared: %q", reloaded.AppleUserToken)
	}
}

// --- helpers ---

func containsSubstr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

// --- Login full success flow ---

func TestLogin_SuccessFlow(t *testing.T) {
	// Override HOME so cfg.Save("") writes to a temp dir.
	t.Setenv("HOME", t.TempDir())

	ln, err := net.Listen("tcp", ":0") //nolint:gosec // G102: ":0" is standard for finding a free port in tests
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{ //nolint:gosec // G101: test credentials, not real secrets
		AppleDeveloperToken: "test-dev-token",
		AuthPort:            port,
		StoreFront:          "us",
		Provider:            "apple",
		Theme:               "default",
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		body, _ := json.Marshal(map[string]string{"user_token": "test-user-token"})
		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		resp, err := http.Post(callbackURL, "application/json", bytes.NewReader(body)) //nolint:gosec // G107: localhost URL constructed in test
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if err := Login(cfg); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if cfg.AppleUserToken != "test-user-token" {
		t.Errorf("AppleUserToken = %q, want %q", cfg.AppleUserToken, "test-user-token")
	}
}

// TestLogin_UsesInjectedDevToken verifies that a build-time injected devToken is
// used when the config has no AppleDeveloperToken set.
func TestLogin_UsesInjectedDevToken(t *testing.T) {
	// Temporarily set the package-level injected token.
	original := devToken
	devToken = "injected-dev-token" //nolint:gosec // G101: test value, not a real credential
	t.Cleanup(func() { devToken = original })

	port := 17782
	cfg := &config.Config{
		AppleDeveloperToken: "", // intentionally blank
		AuthPort:            port,
	}

	// Simulate the browser posting a user token.
	go func() {
		time.Sleep(100 * time.Millisecond)
		body, _ := json.Marshal(map[string]string{"user_token": "injected-flow-token"})
		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		resp, err := http.Post(callbackURL, "application/json", bytes.NewReader(body)) //nolint:gosec // G107: localhost URL in test
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if err := Login(cfg); err != nil {
		t.Fatalf("Login with injected token: %v", err)
	}
	if cfg.AppleUserToken != "injected-flow-token" {
		t.Errorf("AppleUserToken = %q, want %q", cfg.AppleUserToken, "injected-flow-token")
	}
}

// TestLogin_InjectedTokenNotOverridesExplicit verifies that an explicit config token
// takes priority over the injected one.
func TestLogin_InjectedTokenNotOverridesExplicit(t *testing.T) {
	original := devToken
	devToken = "injected-should-be-ignored" //nolint:gosec // G101: test value, not a real credential
	t.Cleanup(func() { devToken = original })

	port := 17783
	cfg := &config.Config{
		AppleDeveloperToken: "explicit-user-set-token",
		AuthPort:            port,
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		body, _ := json.Marshal(map[string]string{"user_token": "explicit-flow-token"})
		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		resp, err := http.Post(callbackURL, "application/json", bytes.NewReader(body)) //nolint:gosec // G107: localhost URL in test
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if err := Login(cfg); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// The developer token in config must remain unchanged (not overwritten).
	if cfg.AppleDeveloperToken != "explicit-user-set-token" {
		t.Errorf("AppleDeveloperToken was overwritten; got %q", cfg.AppleDeveloperToken)
	}
}
