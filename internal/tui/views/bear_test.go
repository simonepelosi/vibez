package views

import (
	"strings"
	"testing"
)

// --- RenderBearLine ---

func TestRenderBearLine_Playing_NonEmpty(t *testing.T) {
	got := RenderBearLine(0, true)
	if got == "" {
		t.Error("RenderBearLine(playing=true) returned empty string")
	}
}

func TestRenderBearLine_Sleeping_NonEmpty(t *testing.T) {
	got := RenderBearLine(0, false)
	if got == "" {
		t.Error("RenderBearLine(playing=false) returned empty string")
	}
}

func TestRenderBearLine_Playing_ContainsVibing(t *testing.T) {
	got := RenderBearLine(0, true)
	if !strings.Contains(got, "vibing") {
		t.Errorf("RenderBearLine(playing=true) should contain 'vibing', got %q", got)
	}
}

func TestRenderBearLine_Sleeping_ContainsZzz(t *testing.T) {
	got := RenderBearLine(0, false)
	if !strings.Contains(got, "zZz") {
		t.Errorf("RenderBearLine(playing=false) should contain 'zZz', got %q", got)
	}
}

func TestRenderBearLine_CyclesFrames_NoPanic(t *testing.T) {
	for step := 0; step < 60; step++ {
		_ = RenderBearLine(step, true)
		_ = RenderBearLine(step, false)
	}
}

// --- BearExpr ---

func TestBearExpr_Playing_NonEmpty(t *testing.T) {
	got := BearExpr(0, true)
	if got == "" {
		t.Error("BearExpr(playing=true) returned empty string")
	}
}

func TestBearExpr_Sleeping_NonEmpty(t *testing.T) {
	got := BearExpr(0, false)
	if got == "" {
		t.Error("BearExpr(playing=false) returned empty string")
	}
}

func TestBearExpr_CyclesNoPanic(t *testing.T) {
	for step := 0; step < 120; step++ {
		_ = BearExpr(step, true)
		_ = BearExpr(step, false)
	}
}

// --- RenderBear ---

func TestRenderBear_Playing_NonEmpty(t *testing.T) {
	got := RenderBear(0, true, 40)
	if got == "" {
		t.Error("RenderBear(playing=true) returned empty string")
	}
}

func TestRenderBear_Sleeping_NonEmpty(t *testing.T) {
	got := RenderBear(0, false, 40)
	if got == "" {
		t.Error("RenderBear(playing=false) returned empty string")
	}
}

func TestRenderBear_ContainsBearLines(t *testing.T) {
	got := RenderBear(0, true, 40)
	// RenderBear returns BearLines lines joined by newline.
	lines := strings.Split(got, "\n")
	if len(lines) != BearLines {
		t.Errorf("RenderBear returned %d lines, want %d", len(lines), BearLines)
	}
}

func TestRenderBear_CyclesNoPanic(t *testing.T) {
	for step := 0; step < 60; step++ {
		_ = RenderBear(step, true, 80)
		_ = RenderBear(step, false, 80)
	}
}

func TestRenderBear_ZeroWidthNoPanic(t *testing.T) {
	// Should not panic even with zero width.
	_ = RenderBear(0, true, 0)
	_ = RenderBear(0, false, 0)
}

func TestRenderBear_FrameWithAboveAnnotation(t *testing.T) {
	// Dance frame index 0 has above="♪" — step 0 selects it.
	got := RenderBear(0, true, 40)
	if got == "" {
		t.Error("RenderBear(step=0, playing=true) returned empty string")
	}
}

func TestRenderBear_FrameWithBelowAnnotation(t *testing.T) {
	// Dance frame index 1 has below="♫" — step 6 selects it (6/6 % 5 == 1).
	got := RenderBear(6, true, 40)
	if got == "" {
		t.Error("RenderBear(step=6, playing=true) returned empty string")
	}
}

func TestRenderBear_SleepFrameWithAnnotation(t *testing.T) {
	// Sleep frame index 0 has above="z" — step 0 selects it (0/12 == 0).
	got := RenderBear(0, false, 40)
	if got == "" {
		t.Error("RenderBear(step=0, playing=false) returned empty string")
	}
}
