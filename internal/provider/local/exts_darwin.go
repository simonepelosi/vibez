//go:build darwin

package local

// platformName is how the running platform is named in user-facing scan
// messages. The supported set differs by platform, so a message that omits it
// cannot be acted on.
const platformName = "macOS"

// ExtAudioFile decodes uncompressed WAV and AIFF natively, at their own bit
// depth and sample rate.
var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".wav":  true,
	".aiff": true,
	".aif":  true,
}
