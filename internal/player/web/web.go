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
		"DeveloperToken":   escapeJSString(devToken),
		"UserToken":        escapeJSString(userToken),
		"Storefront":       escapeJSString(storefront),
		"Version":          escapeJSString(version),
		"AudioBitrateKbps": audioBitrateKbps,
	}); err != nil {
		return "", fmt.Errorf("rendering musickit template: %w", err)
	}
	return buf.String(), nil
}

// escapeJSString encodes s for interpolation in JavaScript string and template literals.
func escapeJSString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '"':
			b.WriteString(`\"`)
		case '`':
			b.WriteString("\\`")
		case '$':
			b.WriteString(`\$`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '<':
			b.WriteString(`\u003c`)
		case '>':
			b.WriteString(`\u003e`)
		case '\u2028':
			b.WriteString(`\u2028`)
		case '\u2029':
			b.WriteString(`\u2029`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
