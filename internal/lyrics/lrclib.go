// Package lyrics fetches and parses lyrics from LRCLIB (https://lrclib.net),
// a free, open, community-maintained lyrics database that requires no API key.
package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

// maxLyricsResponseBytes bounds how much of an LRCLIB response body is read (1 MiB).
const maxLyricsResponseBytes = 1 << 20

// Word is a single timed word or syllable span within an enhanced LRC line.
type Word struct {
	Start time.Duration
	Text  string
}

// Line is a single lyric line with an optional start timestamp.
// For plain (unsynced) lyrics, Start is always 0.
type Line struct {
	Start time.Duration
	Text  string
	Words []Word // populated when enhanced LRC (<mm:ss.xx>) word timestamps are present
}

// Result holds the parsed lyrics returned by the client.
type Result struct {
	Lines  []Line
	Synced bool   // true when Start timestamps are meaningful
	Plain  string // original plain-text fallback (may be empty)
}

const defaultBaseURL = "https://lrclib.net"

// Client is a thin HTTP wrapper around the LRCLIB API.
type Client struct {
	http    *http.Client
	baseURL string
}

// NewClient returns a Client ready for use.
func NewClient() *Client {
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: defaultBaseURL,
	}
}

type lrclibTrack struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	SyncedLyrics string  `json:"syncedLyrics"`
	PlainLyrics  string  `json:"plainLyrics"`
	Instrumental bool    `json:"instrumental"`
}

// Fetch retrieves lyrics for a track. It prefers synced (LRC) lyrics and
// falls back to plain lyrics when timing data is unavailable. When an exact
// lookup against /api/get fails (e.g. due to Apple Music "(Deluxe Edition)" or
// "(feat. ...)" suffixes), it falls back to /api/search with normalized names.
// duration is used as a search hint; pass 0 if unknown.
func (c *Client) Fetch(ctx context.Context, artist, title, album string, duration time.Duration) (*Result, error) {
	res, err := c.fetchExact(ctx, artist, title, album, duration)
	if err == nil {
		return res, nil
	}
	// Fall back to /api/search when exact match is not found.
	if searchRes, searchErr := c.fetchSearch(ctx, artist, title, duration); searchErr == nil {
		return searchRes, nil
	}
	return nil, err
}

func (c *Client) base() string {
	if c.baseURL != "" {
		return strings.TrimRight(c.baseURL, "/")
	}
	return defaultBaseURL
}

func (c *Client) fetchExact(ctx context.Context, artist, title, album string, duration time.Duration) (*Result, error) {
	u, err := url.Parse(c.base() + "/api/get")
	if err != nil {
		return nil, err
	}
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

	var data lrclibTrack
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxLyricsResponseBytes)).Decode(&data); err != nil {
		return nil, err
	}
	return buildResult(data)
}

func (c *Client) fetchSearch(ctx context.Context, artist, title string, duration time.Duration) (*Result, error) {
	cleanedTitle := cleanSearchTerm(title)
	cleanedArtist := cleanSearchTerm(artist)
	if cleanedTitle == "" && cleanedArtist == "" {
		return nil, fmt.Errorf("lyrics not found")
	}

	u, err := url.Parse(c.base() + "/api/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if cleanedTrack := cleanedTitle; cleanedTrack != "" {
		q.Set("track_name", cleanedTrack)
	}
	if cleanedArtist != "" {
		q.Set("artist_name", cleanedArtist)
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lrclib search: status %d", resp.StatusCode)
	}

	var candidates []lrclibTrack
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxLyricsResponseBytes)).Decode(&candidates); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("lyrics not found")
	}

	best := pickBestCandidate(candidates, duration)
	if best == nil {
		return nil, fmt.Errorf("lyrics not found")
	}
	return buildResult(*best)
}

// pickBestCandidate selects the best matching track from a list of LRCLIB
// search results, preferring tracks whose duration is close to targetDur and
// that have time-synced lyrics.
func pickBestCandidate(candidates []lrclibTrack, targetDur time.Duration) *lrclibTrack {
	targetSec := targetDur.Seconds()
	var best *lrclibTrack
	bestScore := -1

	for i := range candidates {
		cand := &candidates[i]
		if !cand.Instrumental && cand.SyncedLyrics == "" && cand.PlainLyrics == "" {
			continue
		}
		score := 0
		if targetSec > 0 && cand.Duration > 0 {
			diff := cand.Duration - targetSec
			if diff < 0 {
				diff = -diff
			}
			switch {
			case diff <= 2:
				score += 40
			case diff <= 5:
				score += 25
			case diff <= 15:
				score += 10
			default:
				continue // reject candidates whose duration differs by >15s
			}
		} else {
			score += 10
		}
		if cand.SyncedLyrics != "" {
			score += 20
		} else if cand.PlainLyrics != "" || cand.Instrumental {
			score += 5
		}
		if score > bestScore {
			bestScore = score
			best = cand
		}
	}
	return best
}

func buildResult(data lrclibTrack) (*Result, error) {
	if data.Instrumental {
		return &Result{Lines: []Line{{Text: "♪  Instrumental  ♪"}}, Plain: "♪ Instrumental ♪"}, nil
	}

	if data.SyncedLyrics != "" {
		lines, err := parseLRC(data.SyncedLyrics)
		if err == nil && len(lines) > 0 {
			return &Result{Lines: lines, Synced: true, Plain: provider.CleanLines(data.PlainLyrics)}, nil
		}
	}

	if data.PlainLyrics != "" {
		var lines []Line
		for l := range strings.SplitSeq(data.PlainLyrics, "\n") {
			lines = append(lines, Line{Text: strings.TrimSpace(provider.Clean(l))})
		}
		return &Result{Lines: lines, Plain: provider.CleanLines(data.PlainLyrics)}, nil
	}

	return nil, fmt.Errorf("no lyrics available")
}

// cleanSearchTerm strips parenthetical/bracketed metadata suffixes commonly
// added by Apple Music (e.g. "(feat. ...)", "[Remastered 2021]", "- Single")
// so LRCLIB's /api/search can match the canonical song title.
func cleanSearchTerm(s string) string {
	s = strings.TrimSpace(s)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}} {
		for {
			start := strings.Index(s, pair[0])
			if start < 0 {
				break
			}
			end := strings.Index(s[start:], pair[1])
			if end < 0 {
				break
			}
			s = strings.TrimSpace(s[:start] + " " + s[start+end+len(pair[1]):])
		}
	}
	for _, suffix := range []string{" - Single", " - EP"} {
		if idx := strings.Index(strings.ToLower(s), strings.ToLower(suffix)); idx > 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return strings.Join(strings.Fields(s), " ")
}

// parseLRC parses lines in standard LRC format ("[mm:ss.xx] text") as well as
// enhanced LRC format with inline word timestamps ("[mm:ss.xx] <mm:ss.xx>word ...").
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
		cleaned := strings.TrimSpace(provider.Clean(raw[idx+1:]))
		text, words := parseInlineWords(cleaned, d)
		lines = append(lines, Line{Start: d, Text: text, Words: words})
	}
	return lines, nil
}

// parseInlineWords extracts enhanced LRC word timestamps ("<mm:ss.xx>") from a
// cleaned lyric line. When no valid inline timestamps are present, it returns
// the original text and a nil Words slice.
func parseInlineWords(s string, lineStart time.Duration) (string, []Word) {
	if !strings.Contains(s, "<") || !strings.Contains(s, ">") {
		return s, nil
	}

	var words []Word
	var plainBuilder strings.Builder
	curStart := lineStart
	hasTimedWord := false
	rest := s

	appendSpan := func(span string) {
		if strings.TrimSpace(span) == "" {
			return
		}
		if len(words) == 0 {
			span = strings.TrimLeft(span, " \t")
		}
		words = append(words, Word{Start: curStart, Text: span})
		plainBuilder.WriteString(span)
	}

	for len(rest) > 0 {
		openIdx := strings.IndexByte(rest, '<')
		if openIdx < 0 {
			appendSpan(rest)
			break
		}
		if openIdx > 0 {
			appendSpan(rest[:openIdx])
		}
		closeIdx := strings.IndexByte(rest[openIdx:], '>')
		if closeIdx < 0 {
			// Unclosed '<': treat remainder as literal text.
			appendSpan(rest[openIdx:])
			break
		}
		tagContent := rest[openIdx+1 : openIdx+closeIdx]
		if ts, err := parseLRCTimestamp(tagContent); err == nil {
			curStart = ts
			hasTimedWord = true
			rest = rest[openIdx+closeIdx+1:]
		} else {
			// Not a valid timestamp tag — keep it as literal text up to '>'.
			appendSpan(rest[:openIdx+closeIdx+1])
			rest = rest[openIdx+closeIdx+1:]
		}
	}

	if !hasTimedWord {
		return s, nil
	}
	if len(words) > 0 {
		words[len(words)-1].Text = strings.TrimRight(words[len(words)-1].Text, " \t")
	}
	return strings.TrimSpace(plainBuilder.String()), words
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
