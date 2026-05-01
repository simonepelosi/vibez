package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateToken_ValidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if !probeURL(srv.URL, "dev", "user") {
		t.Error("expected true for 200 response, got false")
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if probeURL(srv.URL, "dev", "user") {
		t.Error("expected false for 401 response, got true")
	}
}

func TestValidateToken_ForbiddenToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if probeURL(srv.URL, "dev", "user") {
		t.Error("expected false for 403 response, got true")
	}
}

func TestValidateToken_EmptyTokens(t *testing.T) {
	if ValidateToken("", "user") {
		t.Error("expected false for empty devToken")
	}
	if ValidateToken("dev", "") {
		t.Error("expected false for empty userToken")
	}
	if ValidateToken("", "") {
		t.Error("expected false for both empty")
	}
}

func TestValidateToken_NetworkError(t *testing.T) {
	// Point at a port that refuses connections → should return true (assume valid).
	if !probeURL("http://127.0.0.1:1", "dev", "user") {
		t.Error("expected true on network error (assume valid), got false")
	}
}

func TestValidateToken_HeadersSet(t *testing.T) {
	var gotAuth, gotUserToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUserToken = r.Header.Get("Music-User-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probeURL(srv.URL, "mydev", "myuser")

	if gotAuth != "Bearer mydev" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer mydev")
	}
	if gotUserToken != "myuser" {
		t.Errorf("Music-User-Token = %q, want %q", gotUserToken, "myuser")
	}
}
