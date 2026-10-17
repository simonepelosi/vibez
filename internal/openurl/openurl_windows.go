//go:build windows

package openurl

import "golang.org/x/sys/windows"

// Open delegates directly to the Windows URL association, without a command
// shell interpreting query strings or other URL characters.
func Open(url string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
