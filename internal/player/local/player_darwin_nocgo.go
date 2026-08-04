//go:build darwin && !cgo

package local

import "fmt"

type Player struct{}

func New() (*Player, error) {
	return nil, fmt.Errorf("local playback requires CGo on macOS. Build with CGO_ENABLED=1")
}
