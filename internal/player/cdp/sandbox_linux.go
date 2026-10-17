//go:build linux

package cdp

import (
	"os"
	"strconv"
	"strings"
)

// sandboxEnabled reports whether Chromium should run with its sandbox on.
//
// The sandbox is what stops a compromised renderer from acting as the user, and
// vibez's Chrome holds a logged-in Apple session, so it is on by default. It
// is off only where Chromium cannot create one: inside Flatpak (which forbids
// nested user namespaces), as root, and on kernels with unprivileged user
// namespaces disabled or restricted. VIBEZ_NO_SANDBOX=1 forces it off.
func sandboxEnabled() bool {
	if os.Getenv("VIBEZ_NO_SANDBOX") == "1" {
		return false
	}
	return sandboxPossible(os.Geteuid(), fileExists("/.flatpak-info"), procValue)
}

// sandboxPossible is the testable core of sandboxEnabled. procValue reads a
// /proc/sys value, returning "" when it does not exist.
func sandboxPossible(euid int, inFlatpak bool, procValue func(path string) string) bool {
	if euid == 0 || inFlatpak {
		return false
	}
	if n, err := strconv.Atoi(procValue("/proc/sys/user/max_user_namespaces")); err == nil && n == 0 {
		return false
	}
	// Debian-family switch.
	if procValue("/proc/sys/kernel/unprivileged_userns_clone") == "0" {
		return false
	}
	// Ubuntu 23.10+: unprivileged user namespaces are denied to unconfined apps.
	if procValue("/proc/sys/kernel/apparmor_restrict_unprivileged_userns") == "1" {
		return false
	}
	return true
}

func procValue(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // fixed /proc/sys paths
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
