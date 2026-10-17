//go:build linux

package local

// platformName is how the running platform is named in user-facing scan
// messages. The supported set differs by platform, so a message that omits it
// cannot be acted on.
const platformName = "Linux"

var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".ogg":  true,
}
