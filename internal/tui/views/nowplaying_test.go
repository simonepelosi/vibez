package views

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// --- FormatDuration ---

func TestFormatDuration_Zero(t *testing.T) {
	got := FormatDuration(0 * time.Second)
	if got != "0:00" {
		t.Errorf("FormatDuration(0) = %q, want %q", got, "0:00")
	}
}

func TestFormatDuration_OneMinute30(t *testing.T) {
	got := FormatDuration(90 * time.Second)
	if got != "1:30" {
		t.Errorf("FormatDuration(90s) = %q, want %q", got, "1:30")
	}
}

func TestFormatDuration_59Seconds(t *testing.T) {
	got := FormatDuration(59 * time.Second)
	if got != "0:59" {
		t.Errorf("FormatDuration(59s) = %q, want %q", got, "0:59")
	}
}

func TestFormatDuration_LargeValue(t *testing.T) {
	got := FormatDuration((62*60 + 3) * time.Second)
	if got != "62:03" {
		t.Errorf("FormatDuration(62m3s) = %q, want %q", got, "62:03")
	}
}

// --- lipglossWidth ---

func TestLipglossWidth_PlainString(t *testing.T) {
	got := lipglossWidth("hello")
	if got != 5 {
		t.Errorf("lipglossWidth(hello) = %d, want 5", got)
	}
}

func TestLipglossWidth_WithEscapeCodes(t *testing.T) {
	// ANSI escape codes should not count toward width.
	got := lipglossWidth("\x1b[31mred\x1b[0m")
	if got != 3 {
		t.Errorf("lipglossWidth with ANSI = %d, want 3", got)
	}
}

func TestLipglossWidth_Empty(t *testing.T) {
	got := lipglossWidth("")
	if got != 0 {
		t.Errorf("lipglossWidth('') = %d, want 0", got)
	}
}

// --- centerLine ---

func TestCenterLine_ContainsOriginalString(t *testing.T) {
	got := centerLine("hello", 20)
	if !strings.Contains(got, "hello") {
		t.Errorf("centerLine should contain original string, got %q", got)
	}
}

func TestCenterLine_StartsWithSpaces(t *testing.T) {
	got := centerLine("hi", 20)
	if !strings.HasPrefix(got, " ") {
		t.Errorf("centerLine should start with spaces for short string, got %q", got)
	}
}

func TestCenterLine_NoExtraPadWhenWider(t *testing.T) {
	got := centerLine("very long string that exceeds width", 5)
	if !strings.Contains(got, "very long string that exceeds width") {
		t.Errorf("centerLine should still contain original string, got %q", got)
	}
}

// --- centerText ---

func TestCenterText_ContainsText(t *testing.T) {
	got := centerText("hello", 40, 20)
	if !strings.Contains(got, "hello") {
		t.Errorf("centerText should contain text, got %q", got)
	}
}

func TestCenterText_HasLeadingNewlines(t *testing.T) {
	got := centerText("hello", 40, 10)
	if !strings.Contains(got, "\n") {
		t.Errorf("centerText should contain newlines for vertical centering, got %q", got)
	}
}

// --- NowPlayingModel ---

func TestNewNowPlaying_NilState(t *testing.T) {
	m := NewNowPlaying(nil)
	if m == nil {
		t.Fatal("NewNowPlaying(nil) returned nil")
	}
}

func TestNowPlaying_SetSize(t *testing.T) {
	m := NewNowPlaying(nil)
	m.SetSize(80, 24)
	if m.width != 80 || m.height != 24 {
		t.Errorf("SetSize: width=%d height=%d, want 80 24", m.width, m.height)
	}
}

func TestNowPlaying_SetState(t *testing.T) {
	m := NewNowPlaying(nil)
	m.SetState(&player.State{})
}

func TestNowPlaying_View_NoTrack(t *testing.T) {
	m := NewNowPlaying(&player.State{Track: nil})
	m.SetSize(80, 24)
	got := m.View()
	// When there is no track, View returns an empty string — no panic.
	if got != "" {
		t.Errorf("View with nil track should return empty string, got %q", got)
	}
}

func TestNowPlaying_View_WithTrack(t *testing.T) {
	st := &player.State{
		Track: &provider.Track{
			Title:    "Test Track",
			Artist:   "Test Artist",
			Album:    "Test Album",
			Duration: 3 * time.Minute,
		},
		Playing:  true,
		Position: 30 * time.Second,
	}
	m := NewNowPlaying(st)
	m.SetSize(80, 30)
	got := m.View()
	if !strings.Contains(got, "Test Track") {
		t.Errorf("View should contain track title, got %q", got)
	}
	if !strings.Contains(got, "0:30") {
		t.Errorf("View should contain elapsed time, got %q", got)
	}
	if !strings.Contains(got, "3:00") {
		t.Errorf("View should contain total duration, got %q", got)
	}
}

func TestNowPlaying_View_ShowsPausedIcon(t *testing.T) {
	st := &player.State{
		Track:   &provider.Track{Title: "T", Artist: "A", Album: "B", Duration: time.Minute},
		Playing: false,
	}
	m := NewNowPlaying(st)
	m.SetSize(80, 24)
	got := m.View()
	if !strings.Contains(got, "⏸") {
		t.Errorf("Paused state should show pause icon, got %q", got)
	}
}

func TestNowPlaying_Update_NoPanic(t *testing.T) {
	m := NewNowPlaying(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
}
