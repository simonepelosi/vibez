package lastfm

import "github.com/simone-vibes/vibez/internal/config"

// apiKey and apiSecret are the Last.fm application credentials injected at
// build time via -ldflags:
//
//	-X 'github.com/simone-vibes/vibez/internal/lastfm.apiKey=<key>'
//	-X 'github.com/simone-vibes/vibez/internal/lastfm.apiSecret=<secret>'
//
// When empty the user must set lastfm_api_key and lastfm_api_secret in their
// config file (useful for self-built binaries).
var (
	apiKey    string //nolint:gochecknoglobals // intentionally set via ldflags
	apiSecret string //nolint:gochecknoglobals // intentionally set via ldflags
)

// ApplyEmbedded copies the build-time embedded Last.fm API key and secret into
// cfg for runtime use without persisting them to config.json on subsequent saves.
func ApplyEmbedded(cfg *config.Config) {
	cfg.SetRuntimeLastfmCredentials(apiKey, apiSecret)
}
