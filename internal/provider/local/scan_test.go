package local

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

func TestScanDirectoryWithParser_EmitsWalkParseStartAndParseHeartbeats(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"01.flac", "02.flac", "03.flac"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu       sync.Mutex
		progress []ScanProgress
	)
	parseTickSeen := make(chan struct{})
	var closeOnce sync.Once

	parseFn := func(path string) (provider.Track, error) {
		select {
		case <-parseTickSeen:
		case <-time.After(2 * time.Second):
			t.Errorf("timed out waiting for ScanPhaseParse heartbeat while parsing %s", path)
		}
		return provider.Track{
			ID:        "local:" + path,
			LocalPath: path,
			Title:     filepath.Base(path),
		}, nil
	}

	opts := ScanOptions{
		Heartbeat: 5 * time.Millisecond,
		OnProgress: func(p ScanProgress) {
			mu.Lock()
			progress = append(progress, p)
			mu.Unlock()
			if p.Phase == ScanPhaseParse {
				closeOnce.Do(func() { close(parseTickSeen) })
			}
		},
	}

	res, err := scanDirectoryWithParser(context.Background(), dir, true, opts, parseFn)
	if err != nil {
		t.Fatalf("scanDirectoryWithParser: %v", err)
	}
	if len(res.tracks) != 3 {
		t.Fatalf("got %d tracks, want 3", len(res.tracks))
	}

	mu.Lock()
	defer mu.Unlock()

	sawStart := false
	sawParse := false
	for _, p := range progress {
		switch p.Phase {
		case ScanPhaseParseStart:
			sawStart = true
			if p.Found != 3 || p.Cached != 0 || p.TotalToParse != 3 {
				t.Fatalf("unexpected ParseStart snapshot: %+v", p)
			}
		case ScanPhaseParse:
			sawParse = true
			if p.TotalToParse != 3 {
				t.Fatalf("unexpected Parse snapshot TotalToParse: %+v", p)
			}
		}
	}
	if !sawStart {
		t.Fatalf("expected ScanPhaseParseStart event, got %+v", progress)
	}
	if !sawParse {
		t.Fatalf("expected at least one ScanPhaseParse heartbeat, got %+v", progress)
	}
}

func TestStartScanHeartbeat_StopsCleanlyAndObeysContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var (
		mu     sync.Mutex
		phases []ScanPhase
	)
	ticked := make(chan struct{})
	var once sync.Once

	stop := startScanHeartbeat(ctx, 5*time.Millisecond, func() {
		mu.Lock()
		phases = append(phases, ScanPhaseWalk)
		mu.Unlock()
		once.Do(func() { close(ticked) })
	})

	select {
	case <-ticked:
	case <-time.After(2 * time.Second):
		cancel()
		stop()
		t.Fatal("timed out waiting for heartbeat tick")
	}

	cancel()
	stop()
	stop() // idempotent

	mu.Lock()
	countAtStop := len(phases)
	mu.Unlock()
	if countAtStop == 0 || !slices.Contains(phases, ScanPhaseWalk) {
		t.Fatalf("unexpected recorded phases: %+v", phases)
	}
}
