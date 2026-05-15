//go:build darwin

package openurl

import "os/exec"

func Open(url string) error {
	return exec.Command("open", url).Start() //nolint:gosec
}
