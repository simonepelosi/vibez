//go:build windows

package local

const platformName = "Windows"

// Chrome decodes these formats in the Windows local playback backend.
var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".ogg":  true,
}
