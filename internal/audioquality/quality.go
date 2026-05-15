package audioquality

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	StandardKbps = 64
	HighKbps     = 256
	DefaultKbps  = HighKbps
)

const unsupportedMessage = "MusicKit JS/web playback max is 256 kbps AAC; supported values are standard/64 kbps AAC and high/256 kbps AAC"

func Parse(value string) (int, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" || v == "high" || v == "256" || v == "256kbps" || v == "256 kbps" {
		return HighKbps, nil
	}
	if v == "standard" || v == "64" || v == "64kbps" || v == "64 kbps" {
		return StandardKbps, nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return 0, fmt.Errorf("unsupported Apple Music bitrate %d kbps: %s", n, unsupportedMessage)
	}
	return 0, fmt.Errorf("unsupported Apple Music audio quality %q: lossless and hi-res are unavailable through MusicKit JS/web playback; %s", value, unsupportedMessage)
}

func Validate(kbps int) error {
	switch kbps {
	case StandardKbps, HighKbps:
		return nil
	default:
		return fmt.Errorf("unsupported Apple Music bitrate %d kbps: %s", kbps, unsupportedMessage)
	}
}

func ConfigValue(kbps int) (string, error) {
	switch kbps {
	case StandardKbps:
		return "standard", nil
	case HighKbps:
		return "high", nil
	default:
		return "", fmt.Errorf("unsupported Apple Music bitrate %d kbps: %s", kbps, unsupportedMessage)
	}
}

func UnsupportedMessage() string { return unsupportedMessage }
