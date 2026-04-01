package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os/exec"
	"time"

	_ "embed"

	"github.com/simone-vibes/vibez/internal/config"
)

//go:embed web/login.html
var loginHTML []byte

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
  5. Set apple_developer_token in ~/.config/vibez/config.json`)
	}

	tokenCh := make(chan string, 1)
	errCh := make(chan error, 1)

	tmpl, err := template.New("login").Parse(string(loginHTML))
	if err != nil {
		return fmt.Errorf("parsing login template: %w", err)
	}

	mux := buildMux(cfg.AppleDeveloperToken, tmpl, tokenCh, errCh)

	addr := fmt.Sprintf(":%d", cfg.AuthPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("starting auth server on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	loginURL := fmt.Sprintf("http://localhost:%d/login", cfg.AuthPort)
	fmt.Println("Connecting to Apple Music...")
	fmt.Println("Your browser will open to complete the login.")

	_ = exec.Command("xdg-open", loginURL).Start() //nolint:gosec // intentional: opens browser at a known localhost URL

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

// buildMux constructs the HTTP mux for the auth flow. Extracted for testability.
func buildMux(devToken string, tmpl *template.Template, tokenCh chan string, errCh chan error) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, map[string]string{
			"DeveloperToken": devToken,
		}); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload callbackPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
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
