package auth

// devToken is the Apple Music developer JWT injected at build time via -ldflags:
//
//	-X 'github.com/simone-vibes/vibez/internal/auth.devToken=<jwt>'
//
// When empty the user must set apple_developer_token in their config file.
// garble -literals obfuscates this string so it does not appear in plaintext.
var devToken string //nolint:gochecknoglobals // intentionally set via ldflags
