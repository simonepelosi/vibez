//go:build darwin

package cmd

import (
	"fmt"

	"github.com/simone-vibes/vibez/internal/player/browserless"
)

func browserlessBackendLabel(audioBitrateKbps int) string {
	return fmt.Sprintf("Browserless (In-Process Widevine + Core Audio) · %d kbps AAC", audioBitrateKbps)
}

func setupBrowserlessPlatform(p *browserless.Player, _ func(string)) {
	// Darwin media controls: MPRIS is a Linux DBus protocol
}
