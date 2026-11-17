package license

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/netguard"
	"github.com/simone-vibes/vibez/internal/provider"
)

// ErrUnauthorized indicates that Apple Music rejected the credentials (HTTP 401/403).
var ErrUnauthorized = errors.New("unauthorized")

// ErrTrackUnavailable indicates that Apple Music returned no playable stream
// assets for the requested track (e.g. region-restricted or unavailable).
var ErrTrackUnavailable = errors.New("CONTENT_RESTRICTED: track unavailable")

type Client struct {
	cfg        *config.Config
	httpClient *http.Client
}

type PlaybackInfo struct {
	SongID          string
	HLSPlaylistURL  string
	HLSKeyServerURL string
	WidevineCertURL string
	Flavor          string
}

type Asset struct {
	Flavor     string `json:"flavor"`
	URL        string `json:"URL"`
	Bitrate    int    `json:"bitrate"`
	ArtworkURL string `json:"artworkURL"`
}

const (
	maxJSONResponseBytes = 4 << 20 // 4 MiB cap on MZPlay / license JSON responses
	maxCertResponseBytes = 1 << 20 // 1 MiB cap on Widevine server certificate
	maxErrorBodyBytes    = 4096    // 4 KiB cap on upstream error diagnostic bodies
)

func NewClient(cfg *config.Config) *Client {
	return NewClientWithHTTP(cfg, nil)
}

func isAppleServiceHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "apple.com" || strings.HasSuffix(host, ".apple.com")
}

// ValidateUpstreamURL ensures an upstream media/playback URL uses public HTTPS
// (with loopback HTTP/HTTPS allowed only under `go test` harnesses), a valid
// non-SSRF host, and no userinfo or control/format runes.
func ValidateUpstreamURL(rawURL string) error {
	return validateUpstreamURL(rawURL, testing.Testing())
}

func validateUpstreamURL(rawURL string, allowLoopback bool) error {
	if _, err := netguard.ParseSafeURL(rawURL, 0, allowLoopback); err != nil {
		return fmt.Errorf("disallowed upstream URL: %w", err)
	}
	return nil
}

// ValidateAppleServiceURL ensures credential-bearing license/certificate URLs
// point strictly to *.apple.com over public HTTPS (or loopback in tests).
func ValidateAppleServiceURL(rawURL string) error {
	return validateAppleServiceURL(rawURL, testing.Testing())
}

func validateAppleServiceURL(rawURL string, allowLoopback bool) error {
	u, err := netguard.ParseSafeURL(rawURL, 0, allowLoopback)
	if err != nil {
		return fmt.Errorf("disallowed Apple service URL: %w", err)
	}
	host := u.Hostname()
	if (allowLoopback && netguard.IsLoopbackHost(host)) || isAppleServiceHost(host) {
		return nil
	}
	return fmt.Errorf("refusing non-Apple license/certificate host %q", host)
}

func redirectPolicy(requireAppleService bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("stopped after 5 redirects")
		}
		if requireAppleService {
			if err := ValidateAppleServiceURL(req.URL.String()); err != nil {
				return fmt.Errorf("disallowed Apple service redirect target: %w", err)
			}
		} else if err := ValidateUpstreamURL(req.URL.String()); err != nil {
			return fmt.Errorf("disallowed redirect target: %w", err)
		}
		if len(via) > 0 {
			orig := via[0].URL
			if req.URL.Scheme != orig.Scheme || !strings.EqualFold(req.URL.Host, orig.Host) {
				req.Header.Del("Authorization")
				req.Header.Del("X-Apple-Music-User-Token")
				req.Header.Del("Music-User-Token")
				req.Header.Del("Cookie")
			}
		}
		return nil
	}
}

var safeRedirectPolicy = redirectPolicy(false)

// NewClientWithHTTP constructs a license Client using a custom *http.Client
// (or a default pooled 15s-timeout client when httpClient is nil).
func NewClientWithHTTP(cfg *config.Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func defaultHTTPClient() *http.Client {
	var base *http.Transport
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		base = tr.Clone()
	} else {
		base = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	base.MaxIdleConnsPerHost = 16
	return &http.Client{
		Timeout:       15 * time.Second,
		Transport:     netguard.NewSafeRoundTripper(base, 5*time.Second, testing.Testing()),
		CheckRedirect: safeRedirectPolicy,
	}
}

// appleServiceHTTPClient returns a shallow clone of c.HTTPClient() enforcing
// strict Apple-domain redirect validation without cloning connection pools.
func (c *Client) appleServiceHTTPClient() *http.Client {
	base := c.HTTPClient()
	clone := *base
	clone.CheckRedirect = redirectPolicy(true)
	return &clone
}

// HTTPClient returns the underlying *http.Client used for upstream requests.
func (c *Client) HTTPClient() *http.Client {
	if c == nil || c.httpClient == nil {
		return defaultHTTPClient()
	}
	return c.httpClient
}

func readBoundedErrorBody(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, maxErrorBodyBytes))
	return provider.Clean(string(raw))
}

// FetchPlaybackInfo requests playback metadata and HLS stream URLs from Apple MZPlay.
func (c *Client) FetchPlaybackInfo(ctx context.Context, songID string, prefer256k bool) (*PlaybackInfo, error) {
	if songID == "" || strings.ContainsAny(songID, "/\\?#\r\n\x00") {
		return nil, fmt.Errorf("invalid song ID %q", provider.Clean(songID))
	}
	u := "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/webPlayback"
	body, _ := json.Marshal(map[string]any{
		"salableAdamId": songID,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.appleServiceHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("webPlayback request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: webPlayback HTTP %d: %s", ErrUnauthorized, resp.StatusCode, readBoundedErrorBody(resp.Body))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webPlayback failed (HTTP %d): %s", resp.StatusCode, readBoundedErrorBody(resp.Body))
	}

	var result struct {
		SongList []struct {
			SongID          any     `json:"songId"`
			HLSKeyServerURL string  `json:"hls-key-server-url"`
			WidevineCertURL string  `json:"widevine-cert-url"`
			Assets          []Asset `json:"assets"`
		} `json:"songList"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding webPlayback response: %w", err)
	}
	if len(result.SongList) == 0 || len(result.SongList[0].Assets) == 0 {
		return nil, fmt.Errorf("%w (song %s)", ErrTrackUnavailable, songID)
	}

	item := result.SongList[0]
	selectedURL := ""
	selectedFlavor := ""

	// Prefer 256k CTR flavor for streaming if available, otherwise pick first available
	targetFlavor := "28:ctrp256"
	if !prefer256k {
		targetFlavor = "32:ctrp64"
	}

	for _, a := range item.Assets {
		if a.Flavor == targetFlavor {
			selectedURL = a.URL
			selectedFlavor = a.Flavor
			break
		}
	}
	if selectedURL == "" && len(item.Assets) > 0 {
		selectedURL = item.Assets[0].URL
		selectedFlavor = item.Assets[0].Flavor
	}
	if err := ValidateUpstreamURL(selectedURL); err != nil {
		return nil, fmt.Errorf("invalid HLS playlist URL: %w", err)
	}
	if item.HLSKeyServerURL != "" {
		if err := ValidateAppleServiceURL(item.HLSKeyServerURL); err != nil {
			return nil, fmt.Errorf("invalid HLS key server URL: %w", err)
		}
	}
	if item.WidevineCertURL != "" {
		if err := ValidateAppleServiceURL(item.WidevineCertURL); err != nil {
			return nil, fmt.Errorf("invalid Widevine cert URL: %w", err)
		}
	}

	return &PlaybackInfo{
		SongID:          fmt.Sprint(item.SongID),
		HLSPlaylistURL:  selectedURL,
		HLSKeyServerURL: item.HLSKeyServerURL,
		WidevineCertURL: item.WidevineCertURL,
		Flavor:          selectedFlavor,
	}, nil
}

// FetchLibraryAssetURL requests the stream for a library item that has no
// catalog song, such as an iCloud upload. Apple answers with songId -1 and a
// single flavorless asset: a plain file URL with no DRM, so there is no
// playlist or license step.
func (c *Client) FetchLibraryAssetURL(ctx context.Context, libraryID string) (string, error) {
	if libraryID == "" || strings.ContainsAny(libraryID, "/\\?#\r\n\x00") {
		return "", fmt.Errorf("invalid library ID %q", provider.Clean(libraryID))
	}
	u := "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/webPlayback"
	body, _ := json.Marshal(map[string]any{
		"universalLibraryId": libraryID,
		"isLibrary":          true,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.appleServiceHTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("webPlayback request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("%w: webPlayback HTTP %d: %s", ErrUnauthorized, resp.StatusCode, readBoundedErrorBody(resp.Body))
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("webPlayback failed (HTTP %d): %s", resp.StatusCode, readBoundedErrorBody(resp.Body))
	}

	var result struct {
		SongList []struct {
			Assets []Asset `json:"assets"`
		} `json:"songList"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding webPlayback response: %w", err)
	}
	if len(result.SongList) == 0 || len(result.SongList[0].Assets) == 0 || result.SongList[0].Assets[0].URL == "" {
		return "", fmt.Errorf("%w (library item %s)", ErrTrackUnavailable, provider.Clean(libraryID))
	}
	assetURL := result.SongList[0].Assets[0].URL
	if err := ValidateUpstreamURL(assetURL); err != nil {
		return "", fmt.Errorf("invalid library asset URL: %w", err)
	}
	return assetURL, nil
}

// FetchServerCertificate downloads Apple's Widevine service certificate.
func (c *Client) FetchServerCertificate(ctx context.Context, certURL string) ([]byte, error) {
	if certURL == "" {
		certURL = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/widevineCert"
	} else if err := ValidateAppleServiceURL(certURL); err != nil {
		return nil, fmt.Errorf("invalid widevine cert URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.appleServiceHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching widevine certificate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching widevine certificate failed (HTTP %d)", resp.StatusCode)
	}

	certBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxCertResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(certBytes) == 0 || len(certBytes) > maxCertResponseBytes {
		return nil, fmt.Errorf("invalid widevine certificate size (%d bytes)", len(certBytes))
	}
	return certBytes, nil
}

// AcquireLicense exchanges a Widevine license challenge with Apple's license server.
func (c *Client) AcquireLicense(ctx context.Context, licenseURL string, challenge []byte, keyURI, songID string) ([]byte, error) {
	if licenseURL == "" {
		licenseURL = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/acquireWebPlaybackLicense"
	} else if err := ValidateAppleServiceURL(licenseURL); err != nil {
		return nil, fmt.Errorf("invalid license server URL: %w", err)
	}

	challengeB64 := base64.StdEncoding.EncodeToString(challenge)
	payload := map[string]any{
		"challenge":      challengeB64,
		"uri":            keyURI,
		"key-system":     "com.widevine.alpha",
		"adamId":         songID,
		"isLibrary":      false,
		"user-initiated": true,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, licenseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("X-Apple-Renewal", "true")

	resp, err := c.appleServiceHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("acquire license HTTP failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: acquire license HTTP %d: %s", ErrUnauthorized, resp.StatusCode, readBoundedErrorBody(resp.Body))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acquire license failed (HTTP %d): %s", resp.StatusCode, readBoundedErrorBody(resp.Body))
	}

	var respJSON struct {
		License   string `json:"license"`
		ErrorCode int    `json:"errorCode"`
		Status    int    `json:"status"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&respJSON); err != nil {
		return nil, fmt.Errorf("decoding license JSON: %w", err)
	}

	if respJSON.Status != 0 || respJSON.License == "" {
		return nil, fmt.Errorf("license server rejected request (status: %d, errCode: %d)", respJSON.Status, respJSON.ErrorCode)
	}

	licenseBytes, err := base64.StdEncoding.DecodeString(respJSON.License)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 license payload: %w", err)
	}

	return licenseBytes, nil
}

func (c *Client) setHeaders(req *http.Request) {
	devTok, usrTok := c.cfg.GetAppleTokens()
	req.Header.Set("Authorization", "Bearer "+devTok)
	req.Header.Set("X-Apple-Music-User-Token", usrTok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://music.apple.com")
	req.Header.Set("Referer", "https://music.apple.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
}
