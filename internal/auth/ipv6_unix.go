//go:build !windows

package auth

import (
	"errors"
	"syscall"
)

func isIPv6Unavailable(err error) bool {
	return errors.Is(err, syscall.EAFNOSUPPORT) ||
		errors.Is(err, syscall.EADDRNOTAVAIL) ||
		errors.Is(err, syscall.EPROTONOSUPPORT)
}
