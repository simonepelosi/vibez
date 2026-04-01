package auth

import "github.com/simone-vibes/vibez/internal/config"

// devToken is the Apple Music developer JWT injected at build time via -ldflags:
//
//	-X 'github.com/simone-vibes/vibez/internal/auth.devToken=<jwt>'
//
// When empty the user must set apple_developer_token in their config file.
// garble -literals obfuscates this string so it does not appear in plaintext.
var devToken string //nolint:gochecknoglobals // intentionally set via ldflags

// ApplyEmbedded sets cfg.AppleDeveloperToken from the build-time embedded token.
// The embedded token always takes priority over any value in the config file,
// ensuring release binaries use the canonical token rather than a stale one.
func ApplyEmbedded(cfg *config.Config) {
	if devToken != "" {
		cfg.AppleDeveloperToken = devToken
	}
}
