//go:build windows && !cgo

package browserless

import "errors"

func newAudioSink() (audioSink, error) {
	return nil, errors.New("audio sink on windows requires cgo")
}
