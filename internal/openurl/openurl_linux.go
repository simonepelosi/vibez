//go:build linux

package openurl

import "os/exec"

func Open(url string) error {
	return exec.Command("xdg-open", url).Start() //nolint:gosec
}
