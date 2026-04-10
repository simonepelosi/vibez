// Package lyrics fetches and parses lyrics from LRCLIB (https://lrclib.net),
// a free, open, community-maintained lyrics database that requires no API key.
package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Line is a single lyric line with an optional start timestamp.
// For plain (unsynced) lyrics, Start is always 0.
type Line struct {
	Start time.Duration
	Text  string
}

// Result holds the parsed lyrics returned by the client.
type Result struct {
	Lines  []Line
	Synced bool   // true when Start timestamps are meaningful
	Plain  string // original plain-text fallback (may be empty)
}

// Client is a thin HTTP wrapper around the LRCLIB API.
type Client struct {
	http *http.Client
}

// NewClient returns a Client ready for use.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 10 * time.Second}}
}

// Fetch retrieves lyrics for a track. It prefers synced (LRC) lyrics and
// falls back to plain lyrics when timing data is unavailable.
// duration is used as a search hint; pass 0 if unknown.
func (c *Client) Fetch(ctx context.Context, artist, title, album string, duration time.Duration) (*Result, error) {
	u, _ := url.Parse("https://lrclib.net/api/get")
	q := u.Query()
	q.Set("artist_name", artist)
	q.Set("track_name", title)
	if album != "" {
		q.Set("album_name", album)
	}
	if duration > 0 {
		q.Set("duration", strconv.FormatFloat(duration.Seconds(), 'f', 0, 64))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil) //nolint:gosec // G107: URL is constructed from a parsed constant base with safe query params
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "vibez")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("lyrics not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lrclib: status %d", resp.StatusCode)
	}

	var data struct {
		SyncedLyrics string `json:"syncedLyrics"`
		PlainLyrics  string `json:"plainLyrics"`
		Instrumental bool   `json:"instrumental"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if data.Instrumental {
		return &Result{Lines: []Line{{Text: "♪  Instrumental  ♪"}}, Plain: "♪ Instrumental ♪"}, nil
	}

	if data.SyncedLyrics != "" {
		lines, err := parseLRC(data.SyncedLyrics)
		if err == nil && len(lines) > 0 {
			return &Result{Lines: lines, Synced: true, Plain: data.PlainLyrics}, nil
		}
	}

	if data.PlainLyrics != "" {
		var lines []Line
		for l := range strings.SplitSeq(data.PlainLyrics, "\n") {
			lines = append(lines, Line{Text: strings.TrimSpace(l)})
		}
		return &Result{Lines: lines, Plain: data.PlainLyrics}, nil
	}

	return nil, fmt.Errorf("no lyrics available")
}

// parseLRC parses lines in LRC format: [mm:ss.xx] text
func parseLRC(lrc string) ([]Line, error) {
	var lines []Line
	for raw := range strings.SplitSeq(lrc, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.HasPrefix(raw, "[") {
			continue
		}
		idx := strings.Index(raw, "]")
		if idx < 0 {
			continue
		}
		d, err := parseLRCTimestamp(raw[1:idx])
		if err != nil {
			continue
		}
		lines = append(lines, Line{Start: d, Text: strings.TrimSpace(raw[idx+1:])})
	}
	return lines, nil
}

// parseLRCTimestamp parses "mm:ss.xx" or "mm:ss" into a Duration.
func parseLRCTimestamp(s string) (time.Duration, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid timestamp: %s", s)
	}
	mins, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	secParts := strings.SplitN(parts[1], ".", 2)
	secs, err := strconv.Atoi(secParts[0])
	if err != nil {
		return 0, err
	}
	ms := 0
	if len(secParts) == 2 {
		raw := secParts[1]
		for len(raw) < 3 {
			raw += "0"
		}
		if len(raw) > 3 {
			raw = raw[:3]
		}
		ms, err = strconv.Atoi(raw)
		if err != nil {
			return 0, err
		}
	}
	return time.Duration(mins)*time.Minute +
		time.Duration(secs)*time.Second +
		time.Duration(ms)*time.Millisecond, nil
}
