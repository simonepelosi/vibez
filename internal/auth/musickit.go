package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/openurl"
)

//go:embed web/login.html
var loginHTML []byte

// stateHeader carries the per-login state on the /callback request. A custom
// header also forces a CORS preflight, so a page on another origin cannot
// post a token to the callback even if it learns the port.
const stateHeader = "X-Vibez-State"

// maxCallbackBody bounds the /callback request body.
const maxCallbackBody = 64 << 10

// openBrowser opens the login URL; a variable so tests can capture the URL.
var openBrowser = openurl.Open

type callbackPayload struct {
	UserToken string `json:"user_token"`
}

// Login starts the MusicKit auth flow: serves a local web page, opens the browser,
// waits for the user token, then saves it to config.
func Login(cfg *config.Config) error {
	ApplyEmbedded(cfg)

	if cfg.AppleDeveloperToken == "" {
		return errors.New(`apple developer token is not set.

To get one:
  1. Go to https://developer.apple.com/account/resources/authkeys/list
  2. Create a key with "MusicKit" capability
  3. Download the .p8 key file
  4. Run: go run ./scripts/gen-devtoken with the required env vars
  5. Set apple_developer_token in the vibez config file (see vibez --help for its platform path)`)
	}

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	tmpl, err := template.New("login").Parse(string(loginHTML))
	if err != nil {
		return fmt.Errorf("parsing login template: %w", err)
	}

	// state ties /login and /callback to this login attempt, so another
	// program or website cannot post a token of its own.
	state := rand.Text()
	mux := buildMux(cfg.AppleDeveloperToken, state, tmpl, tokenCh, errCh)

	// Loopback only: the page carries the developer token and /callback
	// accepts a user token, neither of which belongs on the network.
	// "localhost" may resolve to ::1, so listen there too when available.
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.AuthPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("starting auth server on %s: %w", addr, err)
	}
	listeners := []net.Listener{ln}
	if ln6, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(cfg.AuthPort))); err == nil {
		listeners = append(listeners, ln6)
	}

	srv := &http.Server{
		Handler:           hostGuard(cfg.AuthPort, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	for _, l := range listeners {
		go func() {
			if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
				select {
				case errCh <- err:
				default:
				}
			}
		}()
	}

	loginURL := fmt.Sprintf("http://localhost:%d/login?state=%s", cfg.AuthPort, state)
	fmt.Println("Connecting to Apple Music...")
	fmt.Println("Your browser will open to complete the login.")

	_ = openBrowser(loginURL) // intentional best-effort browser open

	// Print the fallback URL after a short delay so users whose browser did
	// not open automatically can still complete the flow.
	go func() {
		time.Sleep(4 * time.Second)
		fmt.Printf("\nIf your browser did not open, visit:\n  %s\n\n", loginURL)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	shutdownSrv := func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}

	select {
	case token := <-tokenCh:
		shutdownSrv()
		cfg.AppleUserToken = token
		if err := cfg.Save(""); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Println("✓ Apple Music connected successfully!")
		return nil
	case err := <-errCh:
		shutdownSrv()
		return fmt.Errorf("auth flow error: %w", err)
	case <-ctx.Done():
		shutdownSrv()
		return errors.New("auth timed out after 5 minutes")
	}
}

// Logout clears saved tokens from config.
func Logout(cfg *config.Config) error {
	cfg.AppleUserToken = ""
	if err := cfg.Save(""); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Println("Logged out. Apple Music user token cleared.")
	return nil
}

// stateMatches reports whether got equals the login state, in constant time.
func stateMatches(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// hostGuard refuses requests addressed to any host other than the loopback
// names on port, so a page elsewhere cannot reach the server through a DNS
// name rebound to 127.0.0.1.
func hostGuard(port int, next http.Handler) http.Handler {
	p := strconv.Itoa(port)
	allowed := []string{
		net.JoinHostPort("localhost", p),
		net.JoinHostPort("127.0.0.1", p),
		net.JoinHostPort("::1", p),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, a := range allowed {
			if strings.EqualFold(r.Host, a) {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})
}

// buildMux constructs the HTTP mux for the auth flow. Extracted for testability.
// Both routes require state, which only the page opened by Login knows.
func buildMux(devToken, state string, tmpl *template.Template, tokenCh chan string, errCh chan error) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if !stateMatches(state, r.URL.Query().Get("state")) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		if err := tmpl.Execute(w, map[string]string{
			"DeveloperToken": devToken,
			"State":          state,
		}); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Checked before the body is read, and without touching errCh, so an
		// unauthenticated request can neither plant a token nor abort the login.
		if !stateMatches(state, r.Header.Get(stateHeader)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var payload callbackPayload
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCallbackBody)).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			errCh <- fmt.Errorf("decoding callback: %w", err)
			return
		}
		if payload.UserToken == "" {
			http.Error(w, "empty token", http.StatusBadRequest)
			errCh <- errors.New("received empty user token")
			return
		}
		w.WriteHeader(http.StatusOK)
		tokenCh <- payload.UserToken
	})

	return mux
}
