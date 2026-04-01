package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/assets"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/player/mpris"
	"github.com/simone-vibes/vibez/internal/player/webkit"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/spf13/cobra"
)

var cfgFile string
var debug bool
var memProfiling bool

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
	rootCmd.PersistentFlags().BoolVar(&memProfiling, "mem-profiling", false, "show live RSS for vibez and its Chrome helper in the header")
}

func runTUI(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	auth.ApplyEmbedded(cfg)

	if cfg.AppleDeveloperToken == "" {
		return fmt.Errorf("apple developer token not set.\n\nSet apple_developer_token in ~/.config/vibez/config.json\nor run: go run ./scripts/gen-devtoken")
	}

	// Install icon on every launch (idempotent, best-effort) so it is
	// available for desktop notifications immediately.
	iconPath := assets.InstallIcon()

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

	// Decide the audio backend without downloading anything.
	// Chrome cached → CDP. Chrome absent but amd64 → CDP (will download in TUI).
	// Any other arch → WebKit fallback.
	_, chromeErr := os.Stat(cdp.ChromePath())
	cdpAvailable := chromeErr == nil || runtime.GOARCH == "amd64"

	if cdpAvailable {
		return runCDPFlow(cfg, iconPath, onUserToken, onStorefront)
	}
	return runWebKitFlow(cfg, iconPath, onUserToken, onStorefront)
}

// runCDPFlow starts the TUI immediately with a loading screen, then performs
// auth and engine init in a background goroutine, sending progress messages
// to the running TUI. Chrome runs headless throughout.
func runCDPFlow(cfg *config.Config, iconPath string, onUserToken, onStorefront func(string)) error {
	prog := tea.NewProgram(tui.New(cfg, nil, nil, tui.Options{MemProfiling: memProfiling}), tea.WithAltScreen())

	go func() {
		// Step 1: Ensure Chrome is installed — may download ~130 MB on first run.
		prog.Send(tui.InitStatusMsg("Initializing vibez…"))
		if err := cdp.EnsureBrowser(func(msg string) {
			prog.Send(tui.InitStatusMsg(msg))
		}); err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("browser setup: %w", err)})
			return
		}

		// Step 2: Auth — opens the system browser; TUI shows status.
		if cfg.AppleUserToken == "" {
			prog.Send(tui.InitStatusMsg("Authorizing with Apple Music…"))
			if err := auth.Login(cfg); err != nil {
				prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
				return
			}
		}

		// Step 3: Start engine — Chrome launches headless because token is already set.
		prog.Send(tui.InitStatusMsg("Starting audio engine…"))
		cdpPlayer, err := cdp.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront)
		if err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine: %w", err)})
			return
		}
		cdpPlayer.OnUserToken = onUserToken
		cdpPlayer.OnStorefront = onStorefront

		// CDP's Run() just waits on an internal channel — safe to call from any goroutine.
		go cdpPlayer.Run()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if waitErr := cdpPlayer.WaitReady(ctx); waitErr != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine init: %w", waitErr)})
			cdpPlayer.Terminate()
			return
		}

		if srv, mprisErr := mpris.NewServer(cdpPlayer); mprisErr == nil {
			go func() {
				for st := range cdpPlayer.Subscribe() {
					srv.Update(st)
				}
			}()
		}

		prog.Send(tui.EngineReadyMsg{
			Player:      cdpPlayer,
			Provider:    apple.New(cfg),
			HelperPaths: []string{cdp.HelperPath(), cdp.ChromePath()},
		})
	}()

	_, err := prog.Run()
	return err
}

// runWebKitFlow is the legacy path: GTK must own the main OS thread so auth
// and engine setup happen before the TUI, and BubbleTea runs in a goroutine.
func runWebKitFlow(cfg *config.Config, iconPath string, onUserToken, onStorefront func(string)) error {
	fmt.Fprintln(os.Stderr, "Engine: WebKit + GStreamer (30 s preview)")

	// Auth before engine creation (no loading TUI in WebKit path).
	if cfg.AppleUserToken == "" {
		if err := auth.Login(cfg); err != nil {
			return fmt.Errorf("authentication: %w", err)
		}
	}

	wkPlayer, err := webkit.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront)
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

		m := tui.New(cfg, prov, wkPlayer, tui.Options{MemProfiling: memProfiling})
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, runErr := p.Run()
		tuiErr <- runErr
		wkPlayer.Terminate()
	}()

	wkPlayer.Run() // GTK main loop — must stay on main OS thread.
	return <-tuiErr
}
