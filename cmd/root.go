package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/player/mpris"
	"github.com/simone-vibes/vibez/internal/player/webkit"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/spf13/cobra"
)

var cfgFile string
var debug bool

var rootCmd = &cobra.Command{
	Use:   "vibez",
	Short: "vibez — a vibe-driven music player for your terminal",
	Long:  "vibez — a vibe-driven music player for your terminal.\n\nRun without arguments to open the TUI.",
	RunE:  runTUI,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/vibez/config.json)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")
}

func runTUI(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.AppleDeveloperToken == "" {
		return fmt.Errorf("apple developer token not set — run: vibez auth login")
	}

	// vibezPlayer combines the player.Player playback interface with the
	// lifecycle methods common to both the webkit and CDP backends.
	type vibezPlayer interface {
		player.Player
		Run()
		Terminate()
		WaitReady(ctx context.Context) error
	}

	// onUserToken / onStorefront closures are shared between backends.
	onUserToken := func(token string) {
		cfg.AppleUserToken = token
		if saveErr := cfg.Save(""); saveErr != nil && debug {
			fmt.Fprintf(os.Stderr, "debug: saving user token: %v\n", saveErr)
		}
	}
	onStorefront := func(sf string) {
		if sf != "" && sf != cfg.StoreFront {
			cfg.StoreFront = sf
			if saveErr := cfg.Save(""); saveErr != nil && debug {
				fmt.Fprintf(os.Stderr, "debug: saving storefront: %v\n", saveErr)
			}
		}
	}

	// On first run, Playwright downloads Chrome (~150 MB) into
	// ~/.cache/vibez/playwright — NOT a system install; invisible to apt/snap.
	// Subsequent starts are instant (cached binary, no network).
	// PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=1 prevents it from running
	// apt-get/polkit for system library dependencies.
	var audioEngine vibezPlayer

	if err := cdp.EnsureBrowser(os.Stderr); err != nil {
		if debug {
			fmt.Fprintf(os.Stderr, "debug: browser setup failed (%v); falling back to WebKit+GStreamer\n", err)
		}
	}

	if cdpPlayer, cdpErr := cdp.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront); cdpErr == nil {
		fmt.Fprintln(os.Stderr, "Engine: Chrome + Widevine (full tracks)")
		cdpPlayer.OnUserToken = onUserToken
		cdpPlayer.OnStorefront = onStorefront
		audioEngine = cdpPlayer
	} else {
		fmt.Fprintf(os.Stderr, "Engine: WebKit + GStreamer (30 s preview) — Chrome unavailable: %v\n", cdpErr)
		wkPlayer, wkErr := webkit.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront)
		if wkErr != nil {
			return fmt.Errorf("creating audio engine: %w", wkErr)
		}
		wkPlayer.OnUserToken = onUserToken
		wkPlayer.OnStorefront = onStorefront
		audioEngine = wkPlayer
	}

	prov := apple.New(cfg)
	tuiErr := make(chan error, 1)

	// BubbleTea runs in a goroutine — GTK (WebKit mode) must own the main OS thread.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				tuiErr <- fmt.Errorf("panic: %v", r)
				audioEngine.Terminate()
			}
		}()
		// WaitReady blocks until MusicKit auth completes. WebKit is fast (~10 s).
		// CDP with a saved token is also fast (<10 s) because we skip authorize().
		// CDP first-run needs a visible Chrome window for the user to log in, so
		// we allow up to 10 minutes for that interaction.
		waitTimeout := 30 * time.Second
		if cfg.AppleUserToken == "" {
			waitTimeout = 10 * time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		defer cancel()

		if waitErr := audioEngine.WaitReady(ctx); waitErr != nil {
			tuiErr <- fmt.Errorf("audio engine init: %w", waitErr)
			audioEngine.Terminate()
			return
		}

		// Register vibez as an MPRIS player on the session bus so the desktop
		// environment shows "vibez" (not "Chrome") in its media panel.
		// This is best-effort — if D-Bus is unavailable we just log and continue.
		if srv, mprisErr := mpris.NewServer(audioEngine); mprisErr != nil {
			if debug {
				fmt.Fprintf(os.Stderr, "debug: MPRIS unavailable: %v\n", mprisErr)
			}
		} else {
			defer func() { _ = srv.Close() }()
			go func() {
				for st := range audioEngine.Subscribe() {
					srv.Update(st)
				}
			}()
		}

		m := tui.New(cfg, prov, audioEngine)
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, runErr := p.Run()
		tuiErr <- runErr

		audioEngine.Terminate()
	}()

	// Run blocks until Terminate() is called (GTK loop for WebKit; channel for CDP).
	audioEngine.Run()

	return <-tuiErr
}
