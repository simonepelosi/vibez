// Package web provides the shared MusicKit HTML template used by both the
// WebKit and CDP (Chrome DevTools Protocol) audio backends.
package web

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"

	_ "embed"

	"github.com/simone-vibes/vibez/internal/audioquality"
)

//go:embed musickit.html
var musickitHTML string

// storefrontID matches an Apple Music storefront ID such as "us" or "gb".
var storefrontID = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// RenderHTML renders the MusicKit HTML template with the provided parameters.
func RenderHTML(devToken, userToken, storefront, version string, audioBitrateKbps int) (string, error) {
	if err := audioquality.Validate(audioBitrateKbps); err != nil {
		return "", err
	}
	// The storefront lands inside JS string literals, and text/template does
	// not escape it. Anything that isn't a storefront ID is dropped, leaving
	// MusicKit to resolve the account's own.
	if !storefrontID.MatchString(storefront) {
		storefront = ""
	}
	tmpl, err := template.New("musickit").Parse(musickitHTML)
	if err != nil {
		return "", fmt.Errorf("parsing musickit template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, map[string]any{
		"DeveloperToken":   devToken,
		"UserToken":        userToken,
		"Storefront":       storefront,
		"Version":          version,
		"AudioBitrateKbps": audioBitrateKbps,
	}); err != nil {
		return "", fmt.Errorf("rendering musickit template: %w", err)
	}
	return buf.String(), nil
}
