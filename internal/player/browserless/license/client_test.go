package license

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestFetchLibraryAssetURLSendsUniversalLibraryID(t *testing.T) {
	var sent map[string]any
	c := NewClient(&config.Config{})
	c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		return jsonResponse(`{"songList":[{"songId":-1,"needsPlaybackReporting":true,
			"assets":[{"URL":"https://store-033.blobstore.apple.com/upload.mp3"}]}]}`), nil
	})

	got, err := c.FetchLibraryAssetURL(context.Background(), "i.1OLqOIJVbPx")
	if err != nil {
		t.Fatalf("FetchLibraryAssetURL: %v", err)
	}
	if got != "https://store-033.blobstore.apple.com/upload.mp3" {
		t.Errorf("URL = %q", got)
	}
	if sent["universalLibraryId"] != "i.1OLqOIJVbPx" || sent["isLibrary"] != true {
		t.Errorf("request body = %v, want universalLibraryId and isLibrary", sent)
	}
	if _, ok := sent["salableAdamId"]; ok {
		t.Errorf("request body = %v, must not carry salableAdamId", sent)
	}
}

func TestFetchLibraryAssetURLEmptySongListIsUnavailable(t *testing.T) {
	c := NewClient(&config.Config{})
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"failureType":"1010","songList":[]}`), nil
	})
	if _, err := c.FetchLibraryAssetURL(context.Background(), "i.x"); !errors.Is(err, ErrTrackUnavailable) {
		t.Fatalf("err = %v, want ErrTrackUnavailable", err)
	}
}

func TestValidateUpstreamURL(t *testing.T) {
	valid := []string{
		"https://play.itunes.apple.com/stream.m3u8",
		"http://127.0.0.1:8080/seg.mp4",
		"http://[::1]:9000/seg.mp4",
		"http://localhost/seg.mp4",
	}
	for _, u := range valid {
		if err := ValidateUpstreamURL(u); err != nil {
			t.Errorf("ValidateUpstreamURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"",
		"https://example.com/a\nb",
		"https://example.com/a\x00b",
		"https://user:pass@play.itunes.apple.com/seg.mp4",
		"http://example.com/seg.mp4",
		"file:///etc/passwd",
		"ftp://example.com/file",
		"https:///missing-host",
	}
	for _, u := range invalid {
		if err := ValidateUpstreamURL(u); err == nil {
			t.Errorf("ValidateUpstreamURL(%q) = nil, want error", u)
		}
	}
}

func TestSafeRedirectPolicy(t *testing.T) {
	origURL, _ := url.Parse("https://play.itunes.apple.com/api")
	origReq := &http.Request{URL: origURL}

	sameURL, _ := url.Parse("https://play.itunes.apple.com/v2/api")
	sameReq := &http.Request{
		URL: sameURL,
		Header: http.Header{
			"Authorization":            {"Bearer dev"},
			"X-Apple-Music-User-Token": {"mut"},
		},
	}
	if err := safeRedirectPolicy(sameReq, []*http.Request{origReq}); err != nil {
		t.Fatalf("same-origin redirect failed: %v", err)
	}
	if sameReq.Header.Get("Authorization") == "" || sameReq.Header.Get("X-Apple-Music-User-Token") == "" {
		t.Error("same-origin redirect unexpectedly stripped auth headers")
	}

	crossURL, _ := url.Parse("https://cdn.example.com/stream")
	crossReq := &http.Request{
		URL: crossURL,
		Header: http.Header{
			"Authorization":            {"Bearer dev"},
			"X-Apple-Music-User-Token": {"mut"},
		},
	}
	if err := safeRedirectPolicy(crossReq, []*http.Request{origReq}); err != nil {
		t.Fatalf("cross-origin HTTPS redirect failed: %v", err)
	}
	if crossReq.Header.Get("Authorization") != "" || crossReq.Header.Get("X-Apple-Music-User-Token") != "" {
		t.Error("cross-origin redirect leaked Authorization or X-Apple-Music-User-Token header")
	}

	downgradeURL, _ := url.Parse("http://cdn.example.com/stream")
	if err := safeRedirectPolicy(&http.Request{URL: downgradeURL}, []*http.Request{origReq}); err == nil {
		t.Error("HTTP non-loopback redirect succeeded, want error")
	}

	five := []*http.Request{origReq, origReq, origReq, origReq, origReq}
	if err := safeRedirectPolicy(&http.Request{URL: crossURL}, five); err == nil {
		t.Error("5th redirect succeeded, want error")
	}
}

func TestFetchPlaybackInfo_ValidatesURLsAndSanitizesErrorBody(t *testing.T) {
	c := NewClient(&config.Config{})
	if _, err := c.FetchPlaybackInfo(context.Background(), "../bad\n", true); err == nil {
		t.Error("invalid song ID succeeded, want error")
	}

	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"songList":[{"songId":"123","assets":[{"flavor":"28:ctrp256","URL":"http://evil.example.com/stream.m3u8"}]}]}`), nil
	})
	if _, err := c.FetchPlaybackInfo(context.Background(), "123", true); err == nil {
		t.Error("insecure non-loopback asset URL succeeded, want error")
	}

	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader("bad\x1b[31m red\u202ebidi\x00err")),
		}, nil
	})
	_, err := c.FetchPlaybackInfo(context.Background(), "123", true)
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	msg := err.Error()
	if strings.ContainsAny(msg, "\x1b\x00") || strings.ContainsRune(msg, '\u202e') {
		t.Errorf("error message contains unsanitized control/BiDi chars: %q", msg)
	}
}
