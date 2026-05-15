//go:build darwin

package cmd

import (
	"fmt"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/tui"
)

func runPlatform(cfg *config.Config, _ string, opts tui.Options, onUserToken, onStorefront func(string)) error {
	return runCDPFlow(cfg, opts, onUserToken, onStorefront, cdpPlatformHooks{
		initStatus: "Checking Google Chrome...",
		helperPaths: func() []string {
			return []string{cdp.ChromePath()}
		},
		backend: func() string {
			return fmt.Sprintf("Chrome/CDP · provider: Apple Music · helper: %s", cdp.ChromePath())
		},
	})
}
