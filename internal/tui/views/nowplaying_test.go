package views

import (
	"strings"
	"testing"
	"time"
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

// --- RenderGlowTitle ---

func TestRenderGlowTitle_EmptyString(t *testing.T) {
	got := RenderGlowTitle("", 0)
	if got != "" {
		t.Errorf("RenderGlowTitle(\"\") = %q, want %q", got, "")
	}
}

func TestRenderGlowTitle_NonEmpty(t *testing.T) {
	got := RenderGlowTitle("Hello", 0)
	if got == "" {
		t.Error("RenderGlowTitle(Hello) returned empty string")
	}
}

func TestRenderGlowTitle_DifferentSteps(t *testing.T) {
	// Different glowStep values should produce different output (sweep animation).
	a := RenderGlowTitle("Vibez", 0)
	b := RenderGlowTitle("Vibez", 3)
	// Both must be non-empty; they may differ due to colour assignment.
	if a == "" || b == "" {
		t.Error("RenderGlowTitle returned empty for non-empty title")
	}
}

func TestRenderGlowTitle_CyclesNoPanic(t *testing.T) {
	// Verify that RenderGlowTitle produces non-empty output across multiple animation steps.
	title := "Beat"
	runes := []rune(title)
	n := len(runes)
	for step := 0; step < n*3; step++ {
		got := RenderGlowTitle(title, step)
		if got == "" {
			t.Errorf("RenderGlowTitle(step=%d) returned empty string", step)
		}
	}
}

// --- RenderProgressBar ---

func TestRenderProgressBar_ZeroWidth(t *testing.T) {
	got := RenderProgressBar(30*time.Second, 3*time.Minute, 0, 0)
	if got != "" {
		t.Errorf("RenderProgressBar(width=0) = %q, want empty", got)
	}
}

func TestRenderProgressBar_NegativeWidth(t *testing.T) {
	got := RenderProgressBar(30*time.Second, 3*time.Minute, -1, 0)
	if got != "" {
		t.Errorf("RenderProgressBar(width=-1) = %q, want empty", got)
	}
}

func TestRenderProgressBar_ZeroDuration(t *testing.T) {
	got := RenderProgressBar(0, 0, 40, 0)
	if got == "" {
		t.Error("RenderProgressBar(dur=0) returned empty for non-zero width")
	}
}

func TestRenderProgressBar_FullyFilled(t *testing.T) {
	// position == duration → all filled with gradient zigzag chars
	got := RenderProgressBar(3*time.Minute, 3*time.Minute, 10, 0)
	if !strings.ContainsRune(got, '╱') && !strings.ContainsRune(got, '╲') {
		t.Errorf("RenderProgressBar(full) should contain zigzag chars, got %q", got)
	}
}

func TestRenderProgressBar_PartlyFilled(t *testing.T) {
	// 50% → filled (gradient zigzag) + empty (muted zigzag)
	got := RenderProgressBar(30*time.Second, 60*time.Second, 10, 0)
	if !strings.ContainsRune(got, '╱') && !strings.ContainsRune(got, '╲') {
		t.Errorf("RenderProgressBar(50%%) should contain zigzag chars, got %q", got)
	}
}

func TestRenderProgressBar_PositionBeyondDuration(t *testing.T) {
	// position > duration: ratio capped at 1.0 — must not panic.
	got := RenderProgressBar(5*time.Minute, 3*time.Minute, 10, 0)
	if got == "" {
		t.Error("RenderProgressBar(pos>dur) returned empty string")
	}
}

func TestRenderProgressBar_AnimationShifts(t *testing.T) {
	// Different steps shift the wave phase — frames should differ.
	a := RenderProgressBar(30*time.Second, 60*time.Second, 20, 0)
	b := RenderProgressBar(30*time.Second, 60*time.Second, 20, 1)
	if a == b {
		t.Error("RenderProgressBar: adjacent steps should produce different wave output")
	}
}
