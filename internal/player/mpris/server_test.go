//go:build linux

package mpris

import (
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/simone-vibes/vibez/internal/provider"
)

// isSeek distinguishes a deliberate seek from normal playback progression.

func TestIsSeek_NoPriorSample(t *testing.T) {
	// A zero lastAt means we have never sampled a position yet.
	if isSeek(true, 0, time.Time{}, 30*time.Second, time.Now()) {
		t.Error("first sample must never be treated as a seek")
	}
}

func TestIsSeek_NormalPlaybackDrift(t *testing.T) {
	base := time.Now()
	// One second of playback advances position by ~one second: not a seek.
	if isSeek(true, 10*time.Second, base, 11*time.Second, base.Add(time.Second)) {
		t.Error("normal ~1s playback advance should not be a seek")
	}
}

func TestIsSeek_ForwardSeekWhilePlaying(t *testing.T) {
	base := time.Now()
	// ~1s elapsed but position jumped +10s: a forward seek.
	if !isSeek(true, 10*time.Second, base, 21*time.Second, base.Add(time.Second)) {
		t.Error("forward seek while playing not detected")
	}
}

func TestIsSeek_BackwardSeekWhilePlaying(t *testing.T) {
	base := time.Now()
	if !isSeek(true, 60*time.Second, base, 30*time.Second, base.Add(time.Second)) {
		t.Error("backward seek while playing not detected")
	}
}

func TestIsSeek_SeekWhilePaused(t *testing.T) {
	base := time.Now()
	// Paused: position should not advance, so any jump is a seek.
	if !isSeek(false, 10*time.Second, base, 40*time.Second, base.Add(5*time.Second)) {
		t.Error("seek while paused not detected")
	}
}

func TestIsSeek_PausedNoMovement(t *testing.T) {
	base := time.Now()
	// Paused with an unchanged position, even after real time passes: not a seek.
	if isSeek(false, 10*time.Second, base, 10*time.Second, base.Add(5*time.Second)) {
		t.Error("stationary paused position should not be a seek")
	}
}

func TestIsSeek_SubThresholdJitter(t *testing.T) {
	base := time.Now()
	// A sub-threshold discrepancy (timing jitter) is not a seek.
	if isSeek(true, 10*time.Second, base, 11500*time.Millisecond, base.Add(time.Second)) {
		t.Error("sub-threshold jitter should not be a seek")
	}
}

func TestTrackObjectPath(t *testing.T) {
	tests := []struct {
		name       string
		track      *provider.Track
		wantPrefix string
	}{
		{name: "nil track uses no-track path"},
		{name: "primary ID", track: &provider.Track{ID: "12345"}, wantPrefix: "/org/vibez/track/id_"},
		{name: "catalog ID", track: &provider.Track{CatalogID: "67890"}, wantPrefix: "/org/vibez/track/catalog_"},
		{name: "ID with path separators", track: &provider.Track{ID: "i.track/1"}, wantPrefix: "/org/vibez/track/id_"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := trackObjectPath(test.track)
			if !got.IsValid() {
				t.Fatalf("trackObjectPath(%+v) produced invalid D-Bus path %q", test.track, got)
			}
			if test.track == nil {
				if got != noTrackPath {
					t.Fatalf("trackObjectPath(nil) = %q, want %q", got, noTrackPath)
				}
				return
			}
			if !strings.HasPrefix(string(got), test.wantPrefix) {
				t.Fatalf("trackObjectPath(%+v) = %q, want prefix %q", test.track, got, test.wantPrefix)
			}
			if again := trackObjectPath(test.track); again != got {
				t.Fatalf("trackObjectPath(%+v) is unstable: first %q, then %q", test.track, got, again)
			}
		})
	}
}

func TestTrackObjectPath_DistinguishesIdentitySourcesAndRawIDs(t *testing.T) {
	tracks := []*provider.Track{
		{ID: "i.track/1"},
		{ID: "i_track_1"},
		{ID: "catalog_67890"},
		{CatalogID: "67890"},
		{Title: "a", Artist: "b\x00c", Album: "d", Duration: 1},
		{Title: "a\x00b", Artist: "c", Album: "d", Duration: 1},
	}
	seen := make(map[string]*provider.Track, len(tracks))
	for _, track := range tracks {
		path := trackObjectPath(track)
		if previous, exists := seen[string(path)]; exists {
			t.Fatalf("tracks %+v and %+v share object path %q", previous, track, path)
		}
		seen[string(path)] = track
	}
}

func TestTrackObjectPath_MissingIDUsesStableTrackIdentity(t *testing.T) {
	track := &provider.Track{Title: "Partial Track", Artist: "Artist", Album: "Album", Duration: time.Minute}
	got := trackObjectPath(track)
	if !got.IsValid() {
		t.Fatalf("trackObjectPath() produced invalid D-Bus path %q", got)
	}
	if got == noTrackPath {
		t.Fatalf("trackObjectPath() = no-track path %q for a current track", got)
	}
	if again := trackObjectPath(track); again != got {
		t.Fatalf("trackObjectPath() is unstable: first %q, then %q", got, again)
	}
	other := &provider.Track{Title: "Other Partial Track", Artist: track.Artist, Album: track.Album, Duration: track.Duration}
	if otherPath := trackObjectPath(other); otherPath == got {
		t.Fatalf("distinct partial tracks share object path %q", got)
	}
}

func TestPlayerObj_SetPositionValidatesTrackAndPosition(t *testing.T) {
	current := &provider.Track{ID: "current", Duration: 10 * time.Second}
	currentPath := trackObjectPath(current)
	tests := []struct {
		name      string
		trackPath dbus.ObjectPath
		position  int64
	}{
		{name: "stale track", trackPath: trackObjectPath(&provider.Track{ID: "stale"}), position: int64((5 * time.Second).Microseconds())},
		{name: "negative position", trackPath: currentPath, position: -1},
		{name: "overflowing position", trackPath: currentPath, position: 1<<63 - 1},
		{name: "position beyond track", trackPath: currentPath, position: int64((11 * time.Second).Microseconds())},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctrl := &mockController{}
			srv := &Server{
				currentTrackPath:     currentPath,
				currentTrackDuration: current.Duration,
				hasCurrentTrack:      true,
			}
			obj := &playerObj{ctrl: ctrl, srv: srv}
			if err := obj.setPosition(test.trackPath, test.position); err != nil {
				t.Fatalf("SetPosition returned error: %v", err)
			}
			if ctrl.seekDuration != 0 {
				t.Fatalf("invalid SetPosition sought to %s", ctrl.seekDuration)
			}
		})
	}

	ctrl := &mockController{}
	srv := &Server{
		currentTrackPath:     currentPath,
		currentTrackDuration: current.Duration,
		hasCurrentTrack:      true,
	}
	obj := &playerObj{ctrl: ctrl, srv: srv}
	if err := obj.setPosition(currentPath, int64((5 * time.Second).Microseconds())); err != nil {
		t.Fatalf("SetPosition returned error: %v", err)
	}
	if ctrl.seekDuration != 5*time.Second {
		t.Fatalf("current SetPosition sought to %s, want 5s", ctrl.seekDuration)
	}
}
