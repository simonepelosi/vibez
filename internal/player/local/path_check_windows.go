//go:build windows

package local

import "os"

func verifyOpenRegularAudio(_ string, _ os.FileInfo) error {
	return nil
}
