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

	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	scanCacheVersion  = 1
	maxScanWorkers    = 8
	maxScanCacheBytes = 64 << 20 // 64 MiB cap on persisted directory index
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
	if err := ctx.Err(); err != nil {
		return scanResult{}, err
	}

	root := canonicalMusicDir(dir)
	skipped := map[string]int{}
	var candidates []scanCandidate

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
		return nil
	})
	if err != nil {
		return scanResult{}, err
	}

	cachePath := scanCachePath(root)
	cached := loadScanCache(cachePath)

	results := make([]scanItemResult, len(candidates))
	missIdx := make([]int, 0, len(candidates))
	for i, c := range candidates {
		if entry, ok := cached[c.path]; ok && c.size >= 0 && entry.Size == c.size && entry.ModNano == c.modNano {
			t := entry.Track
			t.ID = "local:" + c.path
			t.LocalPath = c.path
			t.Title = provider.Clean(capTagString(t.Title))
			t.Artist = provider.Clean(capTagString(t.Artist))
			t.Album = provider.Clean(capTagString(t.Album))
			t.Genres = provider.CleanAll(t.Genres)
			results[i] = scanItemResult{
				track:     t,
				cachedHit: true,
			}
			continue
		}
		missIdx = append(missIdx, i)
	}

	if err := parseCandidatesParallel(ctx, candidates, missIdx, results); err != nil {
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

func parseCandidatesParallel(ctx context.Context, candidates []scanCandidate, missIdx []int, results []scanItemResult) error {
	if len(missIdx) == 0 {
		return nil
	}
	if len(missIdx) == 1 {
		i := missIdx[0]
		t, err := trackFromFile(candidates[i].path)
		if err != nil {
			results[i].unreadable = true
		} else {
			results[i].track = t
		}
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
				idx := missIdx[pos]
				t, err := trackFromFile(candidates[idx].path)
				if err != nil {
					results[idx].unreadable = true
				} else {
					results[idx].track = t
				}
			}
		})
	}
	wg.Wait()
	return ctx.Err()
}

func scanCachePath(dir string) string {
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		return ""
	}
	clean := filepath.Clean(dir)
	if abs, absErr := filepath.Abs(clean); absErr == nil {
		clean = abs
	}
	sum := sha256.Sum256([]byte(clean))
	name := "local-scan-" + hex.EncodeToString(sum[:8]) + ".json"
	return filepath.Join(cacheRoot, "vibez", name)
}

func loadScanCache(path string) map[string]cachedTrackEntry {
	if path == "" {
		return nil
	}
	f, err := openRegularAudioFile(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxScanCacheBytes+1))
	if err != nil || len(b) == 0 || len(b) > maxScanCacheBytes {
		return nil
	}
	var file scanCacheFile
	if err := json.Unmarshal(b, &file); err != nil || file.Version != scanCacheVersion {
		return nil
	}
	return file.Entries
}

func saveScanCache(path, dir string, entries map[string]cachedTrackEntry) {
	if path == "" {
		return
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
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
	if err := f.Close(); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
