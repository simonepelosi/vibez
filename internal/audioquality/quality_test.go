package audioquality

import (
	"strings"
	"testing"
)

func TestParseSupportedQuality(t *testing.T) {
	tests := map[string]int{"": HighKbps, "high": HighKbps, "256": HighKbps, "standard": StandardKbps, "64": StandardKbps}
	for input, want := range tests {
		got, err := Parse(input)
		if err != nil || got != want {
			t.Fatalf("Parse(%q) = %d, %v; want %d, nil", input, got, err, want)
		}
	}
}

func TestRejectUnsupportedQuality(t *testing.T) {
	for _, input := range []string{"lossless", "hi-res", "1411", "320"} {
		_, err := Parse(input)
		if err == nil {
			t.Fatalf("Parse(%q) succeeded", input)
		}
		if !strings.Contains(err.Error(), "MusicKit JS/web playback max is 256 kbps AAC") {
			t.Fatalf("Parse(%q) error = %q", input, err)
		}
	}
}

func TestConfigValue(t *testing.T) {
	got, err := ConfigValue(StandardKbps)
	if err != nil || got != "standard" {
		t.Fatalf("ConfigValue(64) = %q, %v", got, err)
	}
	got, err = ConfigValue(HighKbps)
	if err != nil || got != "high" {
		t.Fatalf("ConfigValue(256) = %q, %v", got, err)
	}
}
