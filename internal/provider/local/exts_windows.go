//go:build windows

package local

const platformName = "Windows"

// Media Foundation ships built-in decoders and source handlers for MP3, FLAC,
// M4A (AAC/ALAC), and WAV (PCM and IEEE float) on Windows 10+.
var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".wav":  true,
}
