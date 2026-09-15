//go:build darwin && !cgo

package browserless

import "errors"

func newAudioSink() (audioSink, error) {
	return nil, errors.New("audio sink on darwin requires cgo")
}
