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
	"time"

	"github.com/simone-vibes/vibez/internal/config"
)

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

func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// FetchPlaybackInfo requests playback metadata and HLS stream URLs from Apple MZPlay.
func (c *Client) FetchPlaybackInfo(ctx context.Context, songID string, prefer256k bool) (*PlaybackInfo, error) {
	u := "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/webPlayback"
	body, _ := json.Marshal(map[string]any{
		"salableAdamId": songID,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webPlayback request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("webPlayback failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var result struct {
		SongList []struct {
			SongID          any     `json:"songId"`
			HLSKeyServerURL string  `json:"hls-key-server-url"`
			WidevineCertURL string  `json:"widevine-cert-url"`
			Assets          []Asset `json:"assets"`
		} `json:"songList"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding webPlayback response: %w", err)
	}
	if len(result.SongList) == 0 {
		return nil, errors.New("webPlayback returned empty songList")
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

	return &PlaybackInfo{
		SongID:          fmt.Sprint(item.SongID),
		HLSPlaylistURL:  selectedURL,
		HLSKeyServerURL: item.HLSKeyServerURL,
		WidevineCertURL: item.WidevineCertURL,
		Flavor:          selectedFlavor,
	}, nil
}

// FetchServerCertificate downloads Apple's Widevine service certificate.
func (c *Client) FetchServerCertificate(ctx context.Context, certURL string) ([]byte, error) {
	if certURL == "" {
		certURL = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/widevineCert"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching widevine certificate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching widevine certificate failed (HTTP %d)", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// AcquireLicense exchanges a Widevine license challenge with Apple's license server.
func (c *Client) AcquireLicense(ctx context.Context, licenseURL string, challenge []byte, keyURI, songID string) ([]byte, error) {
	if licenseURL == "" {
		licenseURL = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/acquireWebPlaybackLicense"
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acquire license HTTP failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("acquire license failed (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var respJSON struct {
		License   string `json:"license"`
		ErrorCode int    `json:"errorCode"`
		Status    int    `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&respJSON); err != nil {
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
	req.Header.Set("Authorization", "Bearer "+c.cfg.AppleDeveloperToken)
	req.Header.Set("X-Apple-Music-User-Token", c.cfg.AppleUserToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://music.apple.com")
	req.Header.Set("Referer", "https://music.apple.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
}
