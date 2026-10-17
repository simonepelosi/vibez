package discord

import (
	"strings"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

func TestBuildActivity_PlayingTrack(t *testing.T) {
	st := player.State{
		Track: &provider.Track{
			ID:         "test-id",
			Title:      "Starboy",
			Artist:     "The Weeknd",
			Album:      "Starboy",
			Duration:   3*time.Minute + 50*time.Second,
			ArtworkURL: "https://example.com/art/{w}x{h}bb.jpg",
		},
		Playing:  true,
		Position: 30 * time.Second,
	}

	act := BuildActivity(st)
	if act == nil {
		t.Fatal("expected non-nil activity")
	}

	if act.Details != "Starboy" {
		t.Errorf("Details = %q, want %q", act.Details, "Starboy")
	}
	if act.State != "The Weeknd" {
		t.Errorf("State = %q, want %q", act.State, "The Weeknd")
	}
	if act.Assets.LargeText != "Starboy" {
		t.Errorf("LargeText = %q, want %q", act.Assets.LargeText, "Starboy")
	}
	if act.Assets.LargeImage != "https://example.com/art/512x512bb.jpg" {
		t.Errorf("LargeImage = %q, want resolved 512x512 URL", act.Assets.LargeImage)
	}
	if act.Assets.SmallImage != "play" || act.Assets.SmallText != "Playing" {
		t.Errorf("SmallImage/Text = %q/%q, want play/Playing", act.Assets.SmallImage, act.Assets.SmallText)
	}
	if act.Timestamps == nil {
		t.Fatal("expected timestamps for playing track with duration")
	}
	if act.Timestamps.End <= act.Timestamps.Start {
		t.Errorf("Timestamps End (%d) <= Start (%d)", act.Timestamps.End, act.Timestamps.Start)
	}
}

func TestBuildActivity_PausedTrack(t *testing.T) {
	st := player.State{
		Track: &provider.Track{
			ID:     "test-id",
			Title:  "Blinding Lights",
			Artist: "The Weeknd",
		},
		Playing: false,
	}

	act := BuildActivity(st)
	if act == nil {
		t.Fatal("expected non-nil activity")
	}

	if act.Timestamps != nil {
		t.Errorf("expected nil timestamps for paused track, got %+v", act.Timestamps)
	}
	if act.Assets.SmallImage != "pause" || act.Assets.SmallText != "Paused" {
		t.Errorf("SmallImage/Text = %q/%q, want pause/Paused", act.Assets.SmallImage, act.Assets.SmallText)
	}
	if act.Assets.LargeImage != "vibez_logo" {
		t.Errorf("LargeImage = %q, want fallback vibez_logo", act.Assets.LargeImage)
	}
}

func TestBuildActivity_NilTrack(t *testing.T) {
	st := player.State{Track: nil}
	if act := BuildActivity(st); act != nil {
		t.Errorf("expected nil activity for nil track, got %+v", act)
	}
}

func TestSanitizeStr(t *testing.T) {
	// Single char must be padded to at least 2 chars for Discord RPC spec
	if got := sanitizeStr("A"); got != "A " {
		t.Errorf("sanitizeStr('A') = %q, want 'A '", got)
	}

	// Long strings must be truncated to 128 chars
	longStr := strings.Repeat("x", 200)
	got := sanitizeStr(longStr)
	if len([]rune(got)) != 128 {
		t.Errorf("len(sanitizeStr(longStr)) = %d, want 128", len([]rune(got)))
	}

	if got := sanitizeStr(""); got != "" {
		t.Errorf("sanitizeStr('') = %q, want empty", got)
	}
}
