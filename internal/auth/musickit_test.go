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
	"net/url"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
)

// minimalTemplate is a no-op login page template used in handler tests.
const minimalTemplate = `<html><body>{{.DeveloperToken}}</body></html>`

const testState = "test-state"

func newTestMux(t *testing.T) (*http.ServeMux, chan string, chan error) {
	t.Helper()
	tmpl, err := template.New("login").Parse(minimalTemplate)
	if err != nil {
		t.Fatalf("parsing test template: %v", err)
	}
	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := buildMux("TEST_DEV_TOKEN", testState, tmpl, tokenCh, errCh)
	return mux, tokenCh, errCh
}

// callbackRequest builds an authorised POST to /callback.
func callbackRequest(body io.Reader) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/callback", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(stateHeader, testState)
	return req
}

// captureLoginState replaces the browser opener for the test and returns a
// channel that yields the state from the login URL Login opens.
func captureLoginState(t *testing.T) <-chan string {
	t.Helper()
	ch := make(chan string, 1)
	original := openBrowser
	openBrowser = func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("login URL %q: %v", raw, err)
			return err
		}
		ch <- u.Query().Get("state")
		return nil
	}
	t.Cleanup(func() { openBrowser = original })
	return ch
}

// postToken waits for Login to open the browser, then posts token to /callback
// the way the login page does.
func postToken(port int, states <-chan string, token string) {
	state := <-states
	body, _ := json.Marshal(map[string]string{"user_token": token})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost:%d/callback", port), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(stateHeader, state)
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: localhost URL constructed in test
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
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
	req := httptest.NewRequest(http.MethodGet, "/login?state="+testState, nil)
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
	req := callbackRequest(bytes.NewReader(body))
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
	req := callbackRequest(bytes.NewBufferString("{not-json"))
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
	req := callbackRequest(bytes.NewReader(body))
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

// --- state and Host checks ---

func TestLoginHandler_RejectsMissingOrWrongState(t *testing.T) {
	mux, _, _ := newTestMux(t)
	for _, target := range []string{"/login", "/login?state=", "/login?state=wrong"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s: status = %d, want %d", target, w.Code, http.StatusForbidden)
		}
		if containsSubstr(w.Body.String(), "TEST_DEV_TOKEN") {
			t.Errorf("GET %s leaked the developer token", target)
		}
	}
}

func TestCallbackHandler_RejectsMissingOrWrongState(t *testing.T) {
	for _, state := range []string{"", "wrong"} {
		mux, tokenCh, errCh := newTestMux(t)
		body, _ := json.Marshal(callbackPayload{UserToken: "attacker-token"})
		req := httptest.NewRequest(http.MethodPost, "/callback", bytes.NewReader(body))
		if state != "" {
			req.Header.Set(stateHeader, state)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("state %q: status = %d, want %d", state, w.Code, http.StatusForbidden)
		}
		select {
		case tok := <-tokenCh:
			t.Errorf("state %q: token %q was accepted", state, tok)
		default:
		}
		select {
		case err := <-errCh:
			t.Errorf("state %q: unauthenticated request aborted login: %v", state, err)
		default:
		}
	}
}

func TestHostGuard(t *testing.T) {
	h := hostGuard(7777, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	cases := map[string]int{
		"localhost:7777":           http.StatusOK,
		"127.0.0.1:7777":           http.StatusOK,
		"[::1]:7777":               http.StatusOK,
		"rebind-test.example:7777": http.StatusNotFound,
		"localhost:1234":           http.StatusNotFound,
		"192.168.1.20:7777":        http.StatusNotFound,
		"":                         http.StatusNotFound,
	}
	for host, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/login", nil)
		req.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("Host %q: status = %d, want %d", host, w.Code, want)
		}
	}
}

// The sign-in server must not be reachable through a non-loopback address.
func TestLogin_ListensOnLoopbackOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var lanIP string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			lanIP = ipn.IP.String()
			break
		}
	}
	if lanIP == "" {
		t.Skip("no non-loopback IPv4 address on this machine")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	states := captureLoginState(t)
	cfg := &config.Config{AppleDeveloperToken: "test-dev-token", AuthPort: port} //nolint:gosec // G101: test value
	done := make(chan error, 1)
	go func() { done <- Login(cfg) }()

	state := <-states // the server is listening once the browser is opened
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort(lanIP, fmt.Sprint(port)), time.Second); err == nil {
		_ = conn.Close()
		t.Errorf("auth server accepted a connection on %s", lanIP)
	}

	// Finish the login so Login returns.
	stateCh := make(chan string, 1)
	stateCh <- state
	postToken(port, stateCh, "done")
	if err := <-done; err != nil {
		t.Fatalf("Login: %v", err)
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

	states := captureLoginState(t)
	go postToken(port, states, "test-user-token")

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
	states := captureLoginState(t)
	go postToken(port, states, "injected-flow-token")

	if err := Login(cfg); err != nil {
		t.Fatalf("Login with injected token: %v", err)
	}
	if cfg.AppleUserToken != "injected-flow-token" {
		t.Errorf("AppleUserToken = %q, want %q", cfg.AppleUserToken, "injected-flow-token")
	}
}

// TestLogin_EmbeddedTokenOverridesExplicit verifies that the build-time embedded
// token always takes priority over any value in the config, so release binaries
// never use a stale user-supplied token.
func TestLogin_EmbeddedTokenOverridesExplicit(t *testing.T) {
	original := devToken
	devToken = "embedded-takes-priority" //nolint:gosec // G101: test value, not a real credential
	t.Cleanup(func() { devToken = original })

	port := 17783
	cfg := &config.Config{ //nolint:gosec // G101: test value, not a real credential
		AppleDeveloperToken: "stale-config-token",
		AuthPort:            port,
	}

	states := captureLoginState(t)
	go postToken(port, states, "explicit-flow-token")

	if err := Login(cfg); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// The embedded token must override the stale config token.
	if cfg.AppleDeveloperToken != "embedded-takes-priority" {
		t.Errorf("AppleDeveloperToken = %q, want embedded token %q", cfg.AppleDeveloperToken, "embedded-takes-priority")
	}
}
