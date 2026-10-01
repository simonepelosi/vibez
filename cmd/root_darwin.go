//go:build darwin

package cmd

import (
	"fmt"
	"os"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/tui"
)

func runPlatform(cfg *config.Config, _ string, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	wantBrowserless := browserlessFlag || os.Getenv("VIBEZ_BROWSERLESS") == "1"
	if wantBrowserless && browserless.Available() {
		return runBrowserlessFlow(cfg, opts, onUserToken, onStorefront, audioBitrateKbps)
	}
	if cdp.Available() {
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
	if browserless.Available() {
		return runBrowserlessFlow(cfg, opts, onUserToken, onStorefront, audioBitrateKbps)
	}
	return fmt.Errorf("no supported playback engine found: install Google Chrome or use --browserless")
}
