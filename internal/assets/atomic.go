package assets

import (
	"os"
	"path/filepath"
)

// writeAtomicFile writes data to dst via a temporary file in dst's directory
// and an atomic rename so pre-existing symlinks at dst are never followed.
func writeAtomicFile(dst string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}
