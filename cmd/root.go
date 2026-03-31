package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/config"
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

	// Create the WebKit player (does not start GTK yet).
	wkPlayer, err := webkit.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront)
	if err != nil {
		return fmt.Errorf("creating audio engine: %w", err)
	}

	// Persist any user token refreshes that MusicKit JS reports.
	wkPlayer.OnUserToken = func(token string) {
		cfg.AppleUserToken = token
		if saveErr := cfg.Save(""); saveErr != nil && debug {
			fmt.Fprintf(os.Stderr, "debug: saving user token: %v\n", saveErr)
		}
	}
	// Persist the real storefront detected after auth (overrides the "us" default).
	wkPlayer.OnStorefront = func(sf string) {
		if sf != "" && sf != cfg.StoreFront {
			cfg.StoreFront = sf
			if saveErr := cfg.Save(""); saveErr != nil && debug {
				fmt.Fprintf(os.Stderr, "debug: saving storefront: %v\n", saveErr)
			}
		}
	}

	prov := apple.New(cfg)
	tuiErr := make(chan error, 1)

	// BubbleTea runs in a goroutine — GTK must own the main OS thread.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				tuiErr <- fmt.Errorf("panic: %v", r)
				wkPlayer.Terminate()
			}
		}()
		// Wait for MusicKit JS to finish initialising before showing the TUI.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if waitErr := wkPlayer.WaitReady(ctx); waitErr != nil {
			tuiErr <- fmt.Errorf("audio engine init: %w", waitErr)
			wkPlayer.Terminate()
			return
		}

		m := tui.New(cfg, prov, wkPlayer)
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, runErr := p.Run()
		tuiErr <- runErr

		// TUI exited — stop the GTK event loop so Run() returns.
		wkPlayer.Terminate()
	}()

	// GTK event loop on the main OS goroutine — blocks until Terminate().
	wkPlayer.Run()

	return <-tuiErr
}
