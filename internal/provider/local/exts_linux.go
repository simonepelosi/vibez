//go:build linux

package local

var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".ogg":  true,
}
