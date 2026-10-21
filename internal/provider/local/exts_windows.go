//go:build windows

package local

const platformName = "Windows"

// Media Foundation ships built-in decoders for MP3, FLAC, and M4A (AAC/ALAC)
// on Windows 10+.
var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
}
