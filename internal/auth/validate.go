package auth

import (
	"context"
	"net/http"
	"time"
)

const storefrontURL = "https://api.music.apple.com/v1/me/storefront"

// ValidateToken makes a lightweight Apple Music API call to check whether
// the stored user token is still valid.
//
// Returns false when the API responds with 401 or 403 (token expired or
// revoked). Returns true on network errors so that a temporary connectivity
// problem does not force an unnecessary re-login.
func ValidateToken(devToken, userToken string) bool {
	return probeURL(storefrontURL, devToken, userToken)
}

// probeURL is the testable core of ValidateToken. It accepts an explicit URL
// so tests can point it at an httptest server.
func probeURL(url, devToken, userToken string) bool {
	if devToken == "" || userToken == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // URL is a hardcoded constant or test server
	if err != nil {
		return true // assume valid
	}
	req.Header.Set("Authorization", "Bearer "+devToken)
	req.Header.Set("Music-User-Token", userToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true // network error → assume valid, let the engine surface it
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden
}
