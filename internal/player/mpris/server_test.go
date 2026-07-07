//go:build linux

package mpris

import (
	"testing"
	"time"
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
