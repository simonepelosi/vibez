package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type lyricRecord struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
	Instrumental bool    `json:"instrumental"`
}

var featureSuffix = regexp.MustCompile(`(?i)\s*[（(]\s*(?:feat\.?|ft\.?|featuring)\s+[^)）]*[)）]\s*$`)

func baseTitle(title string) string {
	return strings.TrimSpace(featureSuffix.ReplaceAllString(title, ""))
}
func matchingTitle(candidate, title string) bool {
	candidate, title = strings.ToLower(baseTitle(candidate)), strings.ToLower(baseTitle(title))
	if candidate == title {
		return true
	}
	// Bilingual catalogue names use an explicit spaced separator.
	for _, part := range strings.Split(candidate, " - ") {
		if strings.TrimSpace(part) == title {
			return true
		}
	}
	return false
}

// Search the existing provider only after exact metadata lookup misses. A
// title-only language fallback requires a known, matching duration and one
// unambiguous artist; covers and unrelated recordings must never be guessed.
func (c *Client) searchFallback(ctx context.Context, artist, title string, duration time.Duration) (*Result, error) {
	base := c.baseURL
	if base == "" {
		base = "https://lrclib.net"
	}
	title = baseTitle(title)
	for _, includeArtist := range []bool{true, false} {
		if !includeArtist && duration <= 0 {
			break
		}
		q := url.Values{"track_name": {title}}
		if includeArtist {
			q.Set("artist_name", artist)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/search?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "vibez")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		var records []lyricRecord
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("lrclib: status %d", resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(&records)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var selected *lyricRecord
		ambiguous := false
		for i := range records {
			item := &records[i]
			if !matchingTitle(item.TrackName, title) || (duration > 0 && math.Abs(item.Duration-duration.Seconds()) > 3) {
				continue
			}
			if item.SyncedLyrics == "" && item.PlainLyrics == "" && !item.Instrumental {
				continue
			}
			if selected != nil && !strings.EqualFold(selected.ArtistName, item.ArtistName) {
				ambiguous = true
				break
			}
			if selected == nil || (selected.SyncedLyrics == "" && item.SyncedLyrics != "") {
				selected = item
			}
		}
		if !ambiguous && selected != nil {
			return selected.result()
		}
	}
	return nil, errNotFound
}
