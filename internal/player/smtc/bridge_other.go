//go:build !windows || !cgo

package smtc

func newNativeBridge(func(event), func(string)) (bridge, error) {
	return nil, ErrUnavailable
}
