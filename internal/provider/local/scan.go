package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	scanCacheVersion    = 1
	maxScanWorkers      = 8
	maxScanCacheBytes   = 64 << 20 // 64 MiB cap on persisted directory index
	maxCachedGenreCount = 16
)

type cachedTrackEntry struct {
	Size    int64          `json:"size"`
	ModNano int64          `json:"mod_nano"`
	Track   provider.Track `json:"track"`
}

type scanCacheFile struct {
	Version int                         `json:"version"`
	Dir     string                      `json:"dir"`
	Entries map[string]cachedTrackEntry `json:"entries"`
}

type scanCandidate struct {
	path    string
	size    int64
	modNano int64
}

type scanItemResult struct {
	track      provider.Track
	unreadable bool
	cachedHit  bool
}

type scanResult struct {
	tracks     []provider.Track
	skipped    map[string]int
	unreadable int
}

// isLosslessExt reports whether ext can contain lossless audio supported by Matcher.
// Notice .m4a requires inspecting container metadata (alac vs mp4a), which happens
// once during trackFromFile via LocalFormat rather than opening the file twice.
func isLosslessExt(ext string) bool {
	if !supportedExts[ext] {
		return false
	}
	switch ext {
	case ".flac", ".wav", ".aiff", ".aif", ".m4a":
		return true
	default:
		return false
	}
}

func scanDirectory(ctx context.Context, dir string, losslessOnly bool) (scanResult, error) {
	return scanDirectoryWithProgress(ctx, dir, losslessOnly, ScanOptions{})
}

func scanDirectoryWithProgress(ctx context.Context, dir string, losslessOnly bool, opts ScanOptions) (scanResult, error) {
	return scanDirectoryWithParser(ctx, dir, losslessOnly, opts, trackFromFile)
}

func scanDirectoryWithParser(
	ctx context.Context,
	dir string,
	losslessOnly bool,
	opts ScanOptions,
	parseFn func(string) (provider.Track, error),
) (scanResult, error) {
	if err := ctx.Err(); err != nil {
		return scanResult{}, err
	}

	root := canonicalMusicDir(dir)
	skipped := map[string]int{}
	var candidates []scanCandidate
	var walked atomic.Int64
	var found atomic.Int64

	stopWalkHeartbeat := func() {}
	if opts.OnProgress != nil {
		heartbeat := opts.Heartbeat
		if heartbeat <= 0 {
			heartbeat = DefaultScanHeartbeat
		}
		stopWalkHeartbeat = startScanHeartbeat(ctx, heartbeat, func() {
			opts.OnProgress(ScanProgress{
				Phase:  ScanPhaseWalk,
				Walked: int(walked.Load()),
				Found:  int(found.Load()),
			})
		})
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if os.IsPermission(err) {
				if d == nil || d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return err
		}
		walked.Add(1)
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !supportedExts[ext] {
			skipped[ext]++
			return nil
		}
		if losslessOnly && !isLosslessExt(ext) {
			return nil
		}

		cand := scanCandidate{path: path, size: -1}
		if info, infoErr := d.Info(); infoErr == nil {
			cand.size = info.Size()
			cand.modNano = info.ModTime().UnixNano()
		}
		candidates = append(candidates, cand)
		found.Add(1)
		return nil
	})
	stopWalkHeartbeat()
	if err != nil {
		return scanResult{}, err
	}

	cachePath := scanCachePath(root)
	cached := loadScanCache(cachePath, root)

	results := make([]scanItemResult, len(candidates))
	missIdx := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if entry, ok := cached[c.path]; ok && c.size >= 0 && entry.Size == c.size && entry.ModNano == c.modNano {
			t := sanitizeCachedTrack(c.path, entry.Track)
			results[i] = scanItemResult{
				track:     t,
				cachedHit: true,
			}
			continue
		}
		missIdx = append(missIdx, i)
	}

	cachedCount := len(candidates) - len(missIdx)
	if opts.OnProgress != nil {
		opts.OnProgress(ScanProgress{
			Phase:        ScanPhaseParseStart,
			Walked:       int(walked.Load()),
			Found:        len(candidates),
			Cached:       cachedCount,
			TotalToParse: len(missIdx),
		})
	}

	if err := parseCandidatesParallel(ctx, candidates, missIdx, results, opts, cachedCount, int(walked.Load()), parseFn); err != nil {
		return scanResult{}, err
	}

	tracks := make([]provider.Track, 0, len(candidates))
	unreadable := 0
	readableCount := 0
	cacheDirty := false
	for i, res := range results {
		c := candidates[i]
		if res.unreadable {
			unreadable++
			if _, hadCached := cached[c.path]; hadCached {
				cacheDirty = true
			}
			continue
		}
		readableCount++
		if !res.cachedHit && c.size >= 0 {
			cacheDirty = true
		}
		if losslessOnly && strings.ToLower(filepath.Ext(c.path)) == ".m4a" && res.track.LocalFormat != "ALAC" {
			continue
		}
		tracks = append(tracks, res.track)
	}
	if !losslessOnly && len(cached) != readableCount {
		cacheDirty = true
	}

	if cacheDirty && cachePath != "" {
		// Rebuild the persisted cache map only when dirty, keeping lossy
		// entries intact when losslessOnly=true.
		updatedEntries := make(map[string]cachedTrackEntry, max(len(cached), len(candidates)))
		if losslessOnly {
			maps.Copy(updatedEntries, cached)
		}
		for i, res := range results {
			c := candidates[i]
			if res.unreadable {
				delete(updatedEntries, c.path)
				continue
			}
			if c.size >= 0 {
				updatedEntries[c.path] = cachedTrackEntry{
					Size:    c.size,
					ModNano: c.modNano,
					Track:   res.track,
				}
			}
		}
		saveScanCache(cachePath, root, updatedEntries)
	}

	return scanResult{
		tracks:     tracks,
		skipped:    skipped,
		unreadable: unreadable,
	}, nil
}

func startScanHeartbeat(ctx context.Context, interval time.Duration, tickFn func()) func() {
	if interval <= 0 || tickFn == nil {
		return func() {}
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				tickFn()
			}
		}
	})
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		wg.Wait()
	}
}

func parseCandidatesParallel(
	ctx context.Context,
	candidates []scanCandidate,
	missIdx []int,
	results []scanItemResult,
	opts ScanOptions,
	cachedCount int,
	walkedCount int,
	parseFn func(string) (provider.Track, error),
) error {
	if len(missIdx) == 0 {
		return nil
	}

	var parsed atomic.Int64
	stopHeartbeat := func() {}
	if opts.OnProgress != nil {
		heartbeat := opts.Heartbeat
		if heartbeat <= 0 {
			heartbeat = DefaultScanHeartbeat
		}
		stopHeartbeat = startScanHeartbeat(ctx, heartbeat, func() {
			opts.OnProgress(ScanProgress{
				Phase:        ScanPhaseParse,
				Walked:       walkedCount,
				Found:        len(candidates),
				Cached:       cachedCount,
				Parsed:       int(parsed.Load()),
				TotalToParse: len(missIdx),
			})
		})
	}
	defer stopHeartbeat()

	applyParse := func(idx int) {
		t, err := parseFn(candidates[idx].path)
		if err != nil {
			results[idx].unreadable = true
		} else {
			results[idx].track = t
		}
		parsed.Add(1)
	}

	if len(missIdx) == 1 {
		applyParse(missIdx[0])
		return ctx.Err()
	}

	workers := min(min(max(runtime.GOMAXPROCS(0), 2), maxScanWorkers), len(missIdx))
	var cursor atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for {
				if ctx.Err() != nil {
					return
				}
				pos := int(cursor.Add(1) - 1)
				if pos >= len(missIdx) {
					return
				}
				applyParse(missIdx[pos])
			}
		})
	}
	wg.Wait()
	stopHeartbeat()
	return ctx.Err()
}

func sanitizeCachedTrack(path string, t provider.Track) provider.Track {
	t.ID = "local:" + path
	t.LocalPath = path
	t.Title = provider.Clean(capTagString(t.Title))
	if t.Title == "" {
		t.Title = provider.Clean(capTagString(filepath.Base(path)))
	}
	t.Artist = provider.Clean(capTagString(t.Artist))
	t.Album = provider.Clean(capTagString(t.Album))
	t.Genres = provider.CleanAll(t.Genres)
	if len(t.Genres) > maxCachedGenreCount {
		t.Genres = t.Genres[:maxCachedGenreCount]
	}
	for i := range t.Genres {
		t.Genres[i] = capTagString(t.Genres[i])
	}
	if t.Duration < 0 {
		t.Duration = 0
	}
	if t.DiscNumber < 0 {
		t.DiscNumber = 0
	}
	if t.TrackNumber < 0 {
		t.TrackNumber = 0
	}
	t.ISRC = provider.NormalizeISRC(t.ISRC)
	// Local scans never populate remote Apple Music catalog metadata or URLs.
	t.CatalogID = ""
	t.ArtworkURL = ""
	t.PreviewURL = ""
	t.ContentRating = ""
	t.IsAppleDigitalMaster = false
	t.ReleaseDate = ""
	t.ArtworkPalette = provider.ArtworkPalette{}
	t.AudioVariants = nil
	t.LocalFormat = sanitizeCachedFormat(path, t.LocalFormat)
	return t
}

func sanitizeCachedFormat(path, cachedFmt string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return "FLAC"
	case ".wav":
		return "WAV"
	case ".aiff", ".aif":
		return "AIFF"
	case ".mp3":
		return "MP3"
	case ".ogg":
		return "OGG"
	case ".m4a":
		if cachedFmt == "ALAC" || cachedFmt == "M4A" {
			return cachedFmt
		}
		return LosslessFormatLabel(path)
	default:
		return formatFromExt(path)
	}
}

func scanCachePath(dir string) string {
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" || !filepath.IsAbs(cacheRoot) {
		return ""
	}
	if info, err := os.Lstat(cacheRoot); err == nil {
		// Reject a world-writable cache root unless it is a sticky shared base like /tmp.
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o002 != 0 && info.Mode()&fs.ModeSticky == 0 {
			return ""
		}
	}
	clean := filepath.Clean(dir)
	if abs, absErr := filepath.Abs(clean); absErr == nil {
		clean = abs
	}
	sum := sha256.Sum256([]byte(clean))
	name := "local-scan-" + hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(cacheRoot, "vibez", name)
}

func isSafeCacheNode(path string, wantDir bool) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		return false
	}
	if wantDir {
		if !info.IsDir() {
			return false
		}
	} else if !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o002 != 0 {
		return false
	}
	return true
}

func loadScanCache(path, expectedDir string) map[string]cachedTrackEntry {
	if !isSafeCacheNode(filepath.Dir(path), true) || !isSafeCacheNode(path, false) {
		return nil
	}
	f, err := openRegularAudioFile(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o002 != 0) {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, maxScanCacheBytes+1))
	if err != nil || len(b) == 0 || len(b) > maxScanCacheBytes {
		return nil
	}
	var file scanCacheFile
	if err := json.Unmarshal(b, &file); err != nil || file.Version != scanCacheVersion {
		return nil
	}
	if expectedDir != "" && file.Dir != filepath.Clean(expectedDir) {
		return nil
	}
	return file.Entries
}

func saveScanCache(path, dir string, entries map[string]cachedTrackEntry) {
	if path == "" || !filepath.IsAbs(path) {
		return
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return
	}
	if !isSafeCacheNode(parent, true) {
		return
	}
	payload, err := json.Marshal(scanCacheFile{
		Version: scanCacheVersion,
		Dir:     filepath.Clean(dir),
		Entries: entries,
	})
	if err != nil {
		return
	}
	f, err := os.CreateTemp(parent, "local-scan-*.tmp")
	if err != nil {
		return
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		return
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
