//go:build windows

package auth

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isIPv6Unavailable(err error) bool {
	return errors.Is(err, windows.WSAEAFNOSUPPORT) ||
		errors.Is(err, windows.WSAEADDRNOTAVAIL) ||
		errors.Is(err, windows.WSAEPROTONOSUPPORT)
}
