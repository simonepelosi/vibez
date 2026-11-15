//go:build !windows && !linux && !darwin

package discord

import (
	"errors"
	"net"
)

func verifyPeerUID(net.Conn) error {
	return errors.New("discord ipc peer UID verification unsupported on this platform")
}
