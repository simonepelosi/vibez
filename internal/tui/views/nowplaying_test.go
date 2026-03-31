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
