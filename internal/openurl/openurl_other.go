//go:build !linux && !darwin && !windows

package openurl

import "fmt"

func Open(rawURL string) error {
	if err := validateURL(rawURL); err != nil {
		return err
	}
	return fmt.Errorf("opening browser is unsupported on this platform: %s", rawURL)
}
