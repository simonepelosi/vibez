//go:build !windows

package local

import (
	"fmt"
	"os"
	"syscall"
)

func verifyOpenRegularAudio(cleaned string, expected os.FileInfo) error {
	fd, err := syscall.Open(cleaned, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil || fd < 0 {
		return fmt.Errorf("local player: open regular audio file: %w", err)
	}
	f := os.NewFile(uintptr(fd), cleaned) // #nosec G115 -- non-negative fd checked above
	defer func() { _ = f.Close() }()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		return fmt.Errorf("local player: audio file identity changed")
	}
	return nil
}
