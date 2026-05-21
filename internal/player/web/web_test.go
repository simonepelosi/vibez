package web

import (
	"strings"
	"testing"
)

func TestRenderHTMLAcceptsSupportedBitrates(t *testing.T) {
	for _, kbps := range []int{64, 256} {
		html, err := RenderHTML("dev", "user", "us", "test", kbps)
		if err != nil {
			t.Fatalf("RenderHTML(%d): %v", kbps, err)
		}
		if html == "" {
			t.Fatalf("RenderHTML(%d) returned empty HTML", kbps)
		}
	}
}

func TestRenderHTMLRejectsUnsupportedBitrate(t *testing.T) {
	_, err := RenderHTML("dev", "user", "us", "test", 320)
	if err == nil || !strings.Contains(err.Error(), "MusicKit JS/web playback max is 256 kbps AAC") {
		t.Fatalf("RenderHTML(320) error = %v", err)
	}
}
