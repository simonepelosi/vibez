//go:build linux

package local

// platformName is how the running platform is named in user-facing scan
// messages. The supported set differs by platform, so a message that omits it
// cannot be acted on.
const platformName = "Linux"

// GStreamer plays WAV and AIFF at their own bit depth and sample rate.
// wavparse ships in gst-plugins-good, like the FLAC, MP3 and M4A demuxers.
// AIFF prefers aiffparse from gst-plugins-bad; without it gst-libav's
// avdemux_aiff still covers 16-bit files.
var supportedExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".m4a":  true,
	".ogg":  true,
	".wav":  true,
	".aiff": true,
	".aif":  true,
}
