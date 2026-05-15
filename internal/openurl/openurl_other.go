//go:build !linux && !darwin

package openurl

import "fmt"

func Open(url string) error {
	return fmt.Errorf("opening browser is unsupported on this platform: %s", url)
}
