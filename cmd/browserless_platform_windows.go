//go:build windows

package cmd

import (
	"fmt"

	"github.com/simone-vibes/vibez/internal/player/browserless"
)

func browserlessBackendLabel(audioBitrateKbps int) string {
	return fmt.Sprintf("Browserless (In-Process Widevine + Media Foundation) · %d kbps AAC", audioBitrateKbps)
}

func setupBrowserlessPlatform(_ *browserless.Player) {
	// Windows media-key integration (SMTC) is not wired up; MPRIS is a Linux DBus protocol.
}
