// Package web provides the shared MusicKit HTML template used by both the
// WebKit and CDP (Chrome DevTools Protocol) audio backends.
package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	_ "embed"

	"github.com/simone-vibes/vibez/internal/audioquality"
)

//go:embed musickit.html
var musickitHTML string

// RenderHTML renders the MusicKit HTML template with the provided parameters.
func RenderHTML(devToken, userToken, storefront, version string, audioBitrateKbps int, languages ...string) (string, error) {
	if err := audioquality.Validate(audioBitrateKbps); err != nil {
		return "", err
	}
	tmpl, err := template.New("musickit").Parse(musickitHTML)
	if err != nil {
		return "", fmt.Errorf("parsing musickit template: %w", err)
	}
	language := ""
	if len(languages) > 0 {
		language = languages[0]
	}
	languageJSON, err := json.Marshal(language)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, map[string]any{
		"LanguageJSON":     string(languageJSON),
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
