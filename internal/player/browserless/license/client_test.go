package license

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
