//go:build windows && !cgo

package local

import "errors"

func openNativeEngine(_ *Player) (engine, error) {
	return nil, errors.New("local playback on windows requires cgo")
}
