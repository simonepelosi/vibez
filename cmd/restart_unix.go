//go:build linux || darwin

package cmd

import (
	"os"
	"syscall"
)

// restartProcess replaces this process with exe, the binary an update just
// installed, keeping its arguments, environment and terminal. It returns only
// when the exec fails.
func restartProcess(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ()) //nolint:gosec // exe is the binary the updater just verified and installed
}
