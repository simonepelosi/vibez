//go:build !windows && !linux && !darwin

package discord

import "net"

func verifyPeerUID(net.Conn) error {
	return nil
}
