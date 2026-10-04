// Package lyrics fetches and parses lyrics from LRCLIB (https://lrclib.net),
// a free, open, community-maintained lyrics database that requires no API key.
package lyrics

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Line is a single lyric line with an optional start timestamp.
// For plain (unsynced) lyrics, Start is always 0.
type Line struct {
	Speaker string
	Start   time.Duration
	Text    string
}

// Result holds the parsed lyrics returned by the client.
type Result struct {
	Lines  []Line
	Synced bool   // true when Start timestamps are meaningful
	Plain  string // original plain-text fallback (may be empty)
}

// Client is a thin HTTP wrapper around the LRCLIB API.
type Client struct {
	http    *http.Client
	baseURL string
}

// NewClient returns a Client ready for use.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 10 * time.Second}}
}

// Fetch retrieves lyrics for a track. It prefers synced (LRC) lyrics and
// falls back to plain lyrics when timing data is unavailable.
// duration is used as a search hint; pass 0 if unknown.
var ErrNotFound = errors.New("lyrics not found")
var errNotFound = ErrNotFound

func (c *Client) Fetch(ctx context.Context, artist, title, album string, duration time.Duration) (*Result, error) {
	result, err := c.fetch(ctx, artist, title, album, duration)
	if errors.Is(err, errNotFound) && (album != "" || duration > 0) {
		result, err = c.fetch(ctx, artist, title, "", 0)
	}
	if errors.Is(err, errNotFound) {
		return c.searchFallback(ctx, artist, title, duration)
	}
	return result, err
}
func (c *Client) fetch(ctx context.Context, artist, title, album string, duration time.Duration) (*Result, error) {
	base := c.baseURL
	if base == "" {
		base = "https://lrclib.net"
	}
	u, _ := url.Parse(base + "/api/get")
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
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lrclib: status %d", resp.StatusCode)
	}

	var data lyricRecord
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return data.result()
}
func (data lyricRecord) result() (*Result, error) {
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
		speaker := ""
		for l := range strings.SplitSeq(data.PlainLyrics, "\n") {
			label, text := splitSpeaker(strings.TrimSpace(l))
			if label != "" {
				speaker = label
			}
			lines = append(lines, Line{Text: text, Speaker: speaker})
		}
		return &Result{Lines: lines, Plain: data.PlainLyrics}, nil
	}

	return nil, errNotFound
}

// parseLRC parses lines in LRC format: [mm:ss.xx] text
func parseLRC(lrc string) ([]Line, error) {
	var lines []Line
	speaker := ""
	for raw := range strings.SplitSeq(lrc, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.HasPrefix(raw, "[") {
			continue
		}
		// LRC may reuse one lyric across multiple timestamps.
		var starts []time.Duration
		for strings.HasPrefix(raw, "[") {
			idx := strings.Index(raw, "]")
			if idx < 0 {
				break
			}
			d, err := parseLRCTimestamp(raw[1:idx])
			if err != nil {
				break
			}
			starts = append(starts, d)
			raw = strings.TrimSpace(raw[idx+1:])
		}
		if len(starts) == 0 {
			continue
		}
		label, text := splitSpeaker(raw)
		if label != "" {
			speaker = label
		}
		for _, start := range starts {
			lines = append(lines, Line{Start: start, Text: text, Speaker: speaker})
		}
	}
	slices.SortStableFunc(lines, func(a, b Line) int { return cmp.Compare(a.Start, b.Start) })
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
