//go:build darwin

package discord

import (
	"fmt"
	"math"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func verifyPeerUID(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("unexpected connection type %T", conn)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Xucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		if fd > math.MaxInt {
			credErr = fmt.Errorf("invalid socket fd")
			return
		}
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	uid := os.Getuid()
	if cred == nil || uid < 0 || int64(cred.Uid) != int64(uid) {
		return fmt.Errorf("discord ipc socket peer UID mismatch")
	}
	return nil
}
