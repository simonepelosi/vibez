//go:build linux

package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/player/mpris"
	"github.com/simone-vibes/vibez/internal/player/webkit"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	"github.com/simone-vibes/vibez/internal/tui"
)

func runPlatform(cfg *config.Config, iconPath string, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	_, chromeErr := os.Stat(cdp.ChromePath())
	cdpAvailable := chromeErr == nil || runtime.GOARCH == "amd64"

	if cdpAvailable {
		return runCDPFlow(cfg, opts, onUserToken, onStorefront, audioBitrateKbps, cdpPlatformHooks{
			initStatus: "Initializing vibez...",
			afterReady: func(cdpPlayer *cdp.Player) {
				if srv, mprisErr := mpris.NewServer(cdpPlayer); mprisErr == nil {
					go func() {
						for st := range cdpPlayer.Subscribe() {
							srv.Update(st)
						}
					}()
				}
			},
			helperPaths: func() []string {
				return []string{cdp.HelperPath(), cdp.ChromePath()}
			},
			backend: func(audioBitrateKbps int) string {
				return fmt.Sprintf("Chrome/CDP · provider: Apple Music · %d kbps AAC · helper: %s", audioBitrateKbps, cdp.HelperPath())
			},
		})
	}
	return runWebKitFlow(cfg, iconPath, opts, onUserToken, onStorefront, audioBitrateKbps)
}

// runWebKitFlow is the legacy path: GTK must own the main OS thread so auth
// and engine setup happen before the TUI, and BubbleTea runs in a goroutine.
func runWebKitFlow(cfg *config.Config, iconPath string, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	fmt.Fprintln(os.Stderr, "Engine: WebKit + GStreamer (30 s preview, bitrate changes unsupported)")

	if cfg.AppleUserToken != "" {
		fmt.Fprintln(os.Stderr, "Checking Apple Music session...")
		if !auth.ValidateToken(cfg.AppleDeveloperToken, cfg.AppleUserToken) {
			fmt.Fprintln(os.Stderr, "Session expired - re-authenticating...")
			cfg.AppleUserToken = ""
			_ = cfg.Save("")
		}
	}
	if cfg.AppleUserToken == "" {
		if err := auth.Login(cfg); err != nil {
			return fmt.Errorf("authentication: %w", err)
		}
	}

	wkPlayer, err := webkit.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront, audioBitrateKbps)
	if err != nil {
		return fmt.Errorf("creating audio engine: %w", err)
	}
	wkPlayer.OnUserToken = onUserToken
	wkPlayer.OnStorefront = onStorefront

	prov := apple.New(cfg)
	tuiErr := make(chan error, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if waitErr := wkPlayer.WaitReady(ctx); waitErr != nil {
			tuiErr <- fmt.Errorf("audio engine init: %w", waitErr)
			wkPlayer.Terminate()
			return
		}

		if srv, mprisErr := mpris.NewServer(wkPlayer); mprisErr == nil {
			defer func() { _ = srv.Close() }()
			go func() {
				for st := range wkPlayer.Subscribe() {
					srv.Update(st)
				}
			}()
		}

		opts.IconPath = iconPath
		opts.Backend = "WebKit/GStreamer · fixed 30s previews · bitrate changes unsupported · provider: Apple Music"
		m := tui.New(cfg, prov, wkPlayer, opts)
		p := tea.NewProgram(m)

		startLastfmScrobbler(cfg, wkPlayer, func(msg string) { p.Send(tui.DebugLogMsg(msg)) })

		_, runErr := p.Run()
		tuiErr <- runErr
		wkPlayer.Terminate()
	}()

	wkPlayer.Run()
	return <-tuiErr
}
