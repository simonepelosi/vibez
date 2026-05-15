//go:build darwin

package cmd

import (
	"fmt"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/tui"
)

func runPlatform(cfg *config.Config, _ string, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	return runCDPFlow(cfg, opts, onUserToken, onStorefront, audioBitrateKbps, cdpPlatformHooks{
		initStatus: "Checking Google Chrome...",
		helperPaths: func() []string {
			return []string{cdp.ChromePath()}
		},
		backend: func(audioBitrateKbps int) string {
			return fmt.Sprintf("Chrome/CDP · provider: Apple Music · %d kbps AAC · helper: %s", audioBitrateKbps, cdp.ChromePath())
		},
	})
}
