package openurl

import (
	"errors"
	"net/url"
	"strings"
)

// validateURL ensures raw is an http or https URL with a non-empty host,
// no leading dash, and no ASCII control characters before handing it to
// an OS URL launcher.
func validateURL(raw string) error {
	if raw == "" || strings.HasPrefix(raw, "-") {
		return errors.New("invalid URL")
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return errors.New("URL contains control characters")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("unsupported URL scheme")
	}
	if u.Hostname() == "" {
		return errors.New("URL missing host")
	}
	return nil
}
