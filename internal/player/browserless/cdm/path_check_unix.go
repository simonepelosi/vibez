//go:build linux || darwin

package cdm

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
)

func validatePlatformOwnershipAndPerms(libraryPath string, fileInfo os.FileInfo) error {
	if fileInfo.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("widevine CDM path %q must not be world-writable", libraryPath)
	}
	if !isTrustedUnixOwner(fileInfo) {
		return fmt.Errorf("widevine CDM path %q is not owned by the current user or root", libraryPath)
	}

	// Never accept a library located directly inside a world-writable directory
	// (even with the sticky bit set, such as /tmp or /var/tmp).
	parent := filepath.Dir(libraryPath)
	parentInfo, err := os.Lstat(parent) //nolint:gosec // G703: parent derived from IsSafeAbsPath-validated path
	if err != nil {
		return fmt.Errorf("invalid Widevine CDM parent directory %q: %w", parent, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("widevine CDM parent %q must be a non-symlink directory", parent)
	}
	if parentInfo.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("widevine CDM parent directory %q must not be world-writable", parent)
	}
	if !isTrustedUnixOwner(parentInfo) {
		return fmt.Errorf("widevine CDM parent directory %q has untrusted owner", parent)
	}

	// Walk remaining ancestors toward "/" guarding against non-sticky
	// world-writable directories and untrusted owners. (Intermediate system
	// symlinks such as /var -> private/var on macOS are permitted via os.Stat.)
	cur := filepath.Dir(parent)
	for cur != "" {
		st, serr := os.Stat(cur) //nolint:gosec // G703: ancestor derived from IsSafeAbsPath-validated path
		if serr != nil {
			return fmt.Errorf("invalid Widevine CDM ancestor directory %q: %w", cur, serr)
		}
		if st.Mode().Perm()&0o002 != 0 && st.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("widevine CDM ancestor directory %q is world-writable", cur)
		}
		if !isTrustedUnixOwner(st) {
			return fmt.Errorf("widevine CDM ancestor directory %q has untrusted owner", cur)
		}
		next := filepath.Dir(cur)
		if next == cur {
			break
		}
		cur = next
	}

	return verifyOpenRegularSameFile(libraryPath, fileInfo)
}

func isTrustedUnixOwner(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	euid := os.Geteuid()
	if euid < 0 || int64(euid) > math.MaxUint32 {
		return false
	}
	return st.Uid == 0 || st.Uid == uint32(euid) // #nosec G115 -- range verified above
}

func openCheckedFile(path string, expected os.FileInfo) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil || fd < 0 {
		return nil, fmt.Errorf("opening Widevine CDM path %q: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path) // #nosec G115 -- non-negative fd verified above
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		_ = f.Close()
		return nil, fmt.Errorf("widevine CDM file identity changed for %q", path)
	}
	return f, nil
}

func verifyOpenRegularSameFile(path string, expected os.FileInfo) error {
	f, err := openCheckedFile(path, expected)
	if err != nil {
		return err
	}
	return f.Close()
}

// OpenRegularNoFollow opens path read-only without following symlinks on Unix,
// verifying via fstat that the opened descriptor matches expected.
func OpenRegularNoFollow(path string) (*os.File, error) {
	info, err := ValidateLibraryPath(path)
	if err != nil {
		return nil, err
	}
	return openCheckedFile(path, info)
}
