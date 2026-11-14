//go:build linux

package openurl

import "os/exec"

func Open(rawURL string) error {
	if err := validateURL(rawURL); err != nil {
		return err
	}
	cmd := exec.Command("xdg-open", rawURL) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
