package local

import (
	"context"
	"os"
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := &Matcher{
		dir:    dir,
		byISRC: make(map[string][]provider.Track),
		byMeta: make(map[metaKey][]provider.Track),
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if os.IsPermission(err) {
				return filepath.SkipDir
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !isLosslessFile(path) {
			return nil
		}
		t, err := trackFromFile(path)
		if err != nil {
			return nil
		}
		m.addTrack(t)
		return nil
	})
	if err != nil {
		return nil, err
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
		if chosen, ok := chooseISRCCandidate(m.byISRC[code], t.Duration); ok {
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
		if c.Duration <= 0 || absDuration(t.Duration-c.Duration) > FallbackDurationTolerance {
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

// chooseISRCCandidate selects a candidate whose duration agrees with wantDur
// within FallbackDurationTolerance. When wantDur is unknown (<= 0) it returns
// the first candidate; when wantDur > 0 and all candidates report disagreeing
// durations (> ±2s), it returns false so clean/live cuts do not silently match.
func chooseISRCCandidate(cands []provider.Track, wantDur time.Duration) (provider.Track, bool) {
	if len(cands) == 0 {
		return provider.Track{}, false
	}
	if wantDur <= 0 {
		return cands[0], true
	}
	var unknown *provider.Track
	for i := range cands {
		c := &cands[i]
		if c.Duration <= 0 {
			if unknown == nil {
				unknown = c
			}
			continue
		}
		if absDuration(wantDur-c.Duration) <= FallbackDurationTolerance {
			return *c, true
		}
	}
	if unknown != nil {
		return *unknown, true
	}
	return provider.Track{}, false
}

func trackFormat(t provider.Track) string {
	if t.LocalFormat != "" {
		return t.LocalFormat
	}
	return LosslessFormatLabel(t.LocalPath)
}

// isLosslessFile reports whether path is a supported lossless audio file on
// the current platform.
func isLosslessFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if !supportedExts[ext] {
		return false
	}
	switch ext {
	case ".flac", ".wav", ".aiff", ".aif":
		return true
	case ".m4a":
		return isLosslessM4A(path)
	default:
		return false
	}
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

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
