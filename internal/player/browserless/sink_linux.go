//go:build linux

package browserless

import "github.com/simone-vibes/vibez/internal/player/gst"

func newAudioSink() (audioSink, error) {
	return gst.New()
}
