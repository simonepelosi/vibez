// Package web provides the shared MusicKit HTML template used by both the
// WebKit and CDP (Chrome DevTools Protocol) audio backends.
package web

import (
	"fmt"
	"strings"
	"text/template"

	_ "embed"
)

//go:embed musickit.html
var musickitHTML string

// RenderHTML renders the MusicKit HTML template with the provided parameters.
func RenderHTML(devToken, userToken, storefront, version string) (string, error) {
	tmpl, err := template.New("musickit").Parse(musickitHTML)
	if err != nil {
		return "", fmt.Errorf("parsing musickit template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, map[string]string{
		"DeveloperToken": devToken,
		"UserToken":      userToken,
		"Storefront":     storefront,
		"Version":        version,
	}); err != nil {
		return "", fmt.Errorf("rendering musickit template: %w", err)
	}
	return buf.String(), nil
}
