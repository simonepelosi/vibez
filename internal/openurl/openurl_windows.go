//go:build windows

package openurl

import "golang.org/x/sys/windows"

// Open delegates directly to the Windows URL association, without a command
// shell interpreting query strings or other URL characters.
func Open(rawURL string) error {
	if err := validateURL(rawURL); err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(rawURL)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
