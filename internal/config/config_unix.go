//go:build !windows

package config

import (
	"os"
	"syscall"
)

// ensurePrivatePermissions tightens an existing config file to 0600 when it
// is a regular file owned by the current user and has group or world bits set.
func ensurePrivatePermissions(path string) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(path, 0o600)
	}
}
