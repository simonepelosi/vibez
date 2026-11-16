package local

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/simone-vibes/vibez/internal/provider"
)

// FallbackDurationTolerance is the maximum duration difference allowed when
// matching a catalog track to a local lossless file via the metadata fallback.
const FallbackDurationTolerance = 2 * time.Second

// MatchResult describes a local lossless file matched to a streaming track.
type MatchResult struct {
	Path   string
	Format string // "FLAC", "ALAC", "WAV", or "AIFF"
	ByISRC bool
}

type metaKey struct {
	artist string
	album  string
	title  string
}

// Matcher indexes lossless audio files in a local music directory for hybrid
// playback substitution.
type Matcher struct {
	dir    string
	count  int
	byISRC map[string][]provider.Track
	byMeta map[metaKey][]provider.Track
}

// NewMatcher scans dir for playable lossless audio files (.flac, .wav, .aiff,
// .aif, and ALAC .m4a) and builds an in-memory lookup index.
func NewMatcher(dir string) (*Matcher, error) {
	return NewMatcherContext(context.Background(), dir)
}

// NewMatcherContext behaves like NewMatcher but aborts directory walking when
// ctx is cancelled.
func NewMatcherContext(ctx context.Context, dir string) (*Matcher, error) {
	res, err := scanDirectory(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	m := &Matcher{
		dir:    dir,
		byISRC: make(map[string][]provider.Track),
		byMeta: make(map[metaKey][]provider.Track),
	}
	for _, t := range res.tracks {
		m.addTrack(t)
	}
	return m, nil
}

// Count returns the number of lossless local files indexed by the matcher.
func (m *Matcher) Count() int {
	if m == nil {
		return 0
	}
	return m.count
}

func (m *Matcher) addTrack(t provider.Track) {
	m.count++
	if code := provider.NormalizeISRC(t.ISRC); code != "" {
		m.byISRC[code] = append(m.byISRC[code], t)
	}
	key := metaKey{
		artist: normalizeMetaField(t.Artist, "unknown artist"),
		album:  normalizeMetaField(t.Album, "unknown album"),
		title:  normalizeMetaField(t.Title, ""),
	}
	if key.artist != "" && key.album != "" && key.title != "" {
		m.byMeta[key] = append(m.byMeta[key], t)
	}
}

// Match looks up a local lossless file for t. It checks ISRC first, falling
// back to normalized artist + album + title + track number + duration
// tolerance only when the fallback yields a single unambiguous candidate.
func (m *Matcher) Match(t provider.Track) (MatchResult, bool) {
	if m == nil || m.count == 0 {
		return MatchResult{}, false
	}

	if code := provider.NormalizeISRC(t.ISRC); code != "" {
		if chosen, ok := chooseISRCCandidate(m.byISRC[code], t); ok {
			return MatchResult{
				Path:   chosen.LocalPath,
				Format: trackFormat(chosen),
				ByISRC: true,
			}, true
		}
	}

	key := metaKey{
		artist: normalizeMetaField(t.Artist, "unknown artist"),
		album:  normalizeMetaField(t.Album, "unknown album"),
		title:  normalizeMetaField(t.Title, ""),
	}
	if key.artist == "" || key.album == "" || key.title == "" {
		return MatchResult{}, false
	}
	if t.TrackNumber <= 0 || t.Duration <= 0 {
		return MatchResult{}, false
	}

	cands := m.byMeta[key]
	var matched []provider.Track
	for _, c := range cands {
		if c.TrackNumber != t.TrackNumber {
			continue
		}
		if t.DiscNumber > 0 && c.DiscNumber > 0 && c.DiscNumber != t.DiscNumber {
			continue
		}
		if c.Duration <= 0 || !durationWithinTolerance(t.Duration, c.Duration, FallbackDurationTolerance) {
			continue
		}
		matched = append(matched, c)
	}
	if len(matched) != 1 {
		return MatchResult{}, false
	}
	return MatchResult{
		Path:   matched[0].LocalPath,
		Format: trackFormat(matched[0]),
		ByISRC: false,
	}, true
}

// chooseISRCCandidate selects a non-ambiguous candidate matching want's ISRC.
// When want.Duration > 0, candidates with known durations must agree within
// FallbackDurationTolerance; unknown-duration candidates (Duration <= 0) or
// missing want durations require title/artist metadata agreement and must
// resolve uniquely so corrupted or mistagged files cannot act as wildcards.
func chooseISRCCandidate(cands []provider.Track, want provider.Track) (provider.Track, bool) {
	if len(cands) == 0 {
		return provider.Track{}, false
	}
	if want.Duration <= 0 {
		return selectUniqueISRCByMeta(cands, want)
	}
	var matched []provider.Track
	var unknown []provider.Track
	for i := range cands {
		c := cands[i]
		if c.Duration <= 0 {
			if isrcMetadataCompatible(c, want) {
				unknown = append(unknown, c)
			}
			continue
		}
		if durationWithinTolerance(want.Duration, c.Duration, FallbackDurationTolerance) {
			matched = append(matched, c)
		}
	}
	if len(matched) == 1 {
		return matched[0], true
	}
	if len(matched) > 1 {
		return selectUniqueISRCByMeta(matched, want)
	}
	if len(unknown) == 1 {
		return unknown[0], true
	}
	return provider.Track{}, false
}

func selectUniqueISRCByMeta(cands []provider.Track, want provider.Track) (provider.Track, bool) {
	var compatible []provider.Track
	for i := range cands {
		if isrcMetadataCompatible(cands[i], want) {
			compatible = append(compatible, cands[i])
		}
	}
	if len(compatible) == 1 {
		return compatible[0], true
	}
	if len(compatible) > 1 {
		if wantAlbum := normalizeMetaField(want.Album, "unknown album"); wantAlbum != "" {
			var albumMatched []provider.Track
			for i := range compatible {
				if metaTextRelated(normalizeMetaField(compatible[i].Album, "unknown album"), wantAlbum) {
					albumMatched = append(albumMatched, compatible[i])
				}
			}
			if len(albumMatched) == 1 {
				return albumMatched[0], true
			}
		}
	}
	return provider.Track{}, false
}

func isrcMetadataCompatible(c, want provider.Track) bool {
	wantTitle := normalizeMetaField(want.Title, "")
	cTitle := normalizeMetaField(c.Title, "")
	if wantTitle != "" && (cTitle == "" || !metaTextRelated(cTitle, wantTitle)) {
		return false
	}
	wantArtist := normalizeMetaField(want.Artist, "unknown artist")
	cArtist := normalizeMetaField(c.Artist, "unknown artist")
	if wantArtist != "" && (cArtist == "" || !metaTextRelated(cArtist, wantArtist)) {
		return false
	}
	return true
}

func metaTextRelated(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.Contains(" "+a+" ", " "+b+" ") || strings.Contains(" "+b+" ", " "+a+" ")
}

func trackFormat(t provider.Track) string {
	if t.LocalFormat != "" {
		return t.LocalFormat
	}
	return LosslessFormatLabel(t.LocalPath)
}

// LosslessFormatLabel returns the human-readable format badge ("FLAC", "ALAC",
// "WAV", "AIFF") for a local audio file path.
func LosslessFormatLabel(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return "FLAC"
	case ".m4a":
		if isLosslessM4A(path) {
			return "ALAC"
		}
		return "M4A"
	case ".wav":
		return "WAV"
	case ".aiff", ".aif":
		return "AIFF"
	default:
		return formatFromExt(path)
	}
}

func normalizeMetaField(s, placeholder string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	out := b.String()
	if placeholder != "" && out == placeholder {
		return ""
	}
	return out
}

func durationWithinTolerance(a, b, tol time.Duration) bool {
	if tol < 0 {
		return false
	}
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return false
	}
	return absDuration(a-b) <= tol
}

func absDuration(d time.Duration) time.Duration {
	if d == math.MinInt64 {
		return math.MaxInt64
	}
	if d < 0 {
		return -d
	}
	return d
}
