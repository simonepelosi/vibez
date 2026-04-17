package lastfm

import "github.com/simone-vibes/vibez/internal/config"

// apiKey and apiSecret are the Last.fm application credentials injected at
// build time via -ldflags:
//
//	-X 'github.com/simone-vibes/vibez/internal/lastfm.apiKey=<key>'
//	-X 'github.com/simone-vibes/vibez/internal/lastfm.apiSecret=<secret>'
//
// When empty the user must set lastfm_api_key and lastfm_api_secret in their
// config file (useful for self-built binaries). garble -literals obfuscates
// these strings in release builds so they do not appear in plaintext.
var (
	apiKey    string //nolint:gochecknoglobals // intentionally set via ldflags
	apiSecret string //nolint:gochecknoglobals // intentionally set via ldflags
)

// ApplyEmbedded copies the build-time embedded Last.fm API key and secret into
// cfg when they are present, taking priority over any values in the config file.
func ApplyEmbedded(cfg *config.Config) {
	if apiKey != "" {
		cfg.LastfmAPIKey = apiKey
	}
	if apiSecret != "" {
		cfg.LastfmAPISecret = apiSecret
	}
}
