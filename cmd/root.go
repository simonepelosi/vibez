package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/assets"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/lastfm"
	demoPlayer "github.com/simone-vibes/vibez/internal/player/demo"
	localPlayer "github.com/simone-vibes/vibez/internal/player/local"
	demoProvider "github.com/simone-vibes/vibez/internal/provider/demo"
	localProvider "github.com/simone-vibes/vibez/internal/provider/local"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/tui/styles"
	"github.com/spf13/cobra"
)

var cfgFile string
var debug bool
var memProfiling bool
var demo bool
var noUpdate bool
var local bool
var musicDir string

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
	rootCmd.PersistentFlags().BoolVar(&demo, "demo", false, "run with built-in fake data — no Apple account or internet required")
	rootCmd.PersistentFlags().BoolVar(&noUpdate, "no-update", false, "skip automatic update check on startup")
	rootCmd.PersistentFlags().BoolVar(&local, "local", false, "run with local music files (no Apple account required)")
	rootCmd.PersistentFlags().StringVar(&musicDir, "music-dir", "", "path to your music directory (saved to config)")
}

func runTUI(_ *cobra.Command, _ []string) error {
	cfgPath, err := config.ConfigPath(cfgFile)
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	audioBitrateKbps, err := cfg.AudioBitrateKbps()
	if err != nil {
		return err
	}

	opts := tui.Options{MemProfiling: memProfiling}

	// Apply theme before creating any TUI model so all panels pick up the palette.
	theme, themeErr := styles.LoadTheme(cfg.Theme, filepath.Dir(cfgPath))
	if themeErr != nil && debug {
		fmt.Fprintf(os.Stderr, "debug: theme: %v (falling back to default)\n", themeErr)
	}
	styles.Apply(theme)

	if local {
		// if --music-dir was passed, it will save to config.
		if musicDir != "" {
			cfg.MusicDir = musicDir
			if saveErr := cfg.Save(cfgFile); saveErr != nil && debug {
				fmt.Fprintf(os.Stderr, "debug: saving music dir: %v\n", saveErr)
			}
		}

		// If musicDir is still empty, prompt the user on the command line
		// before launching the TUI.
		if cfg.MusicDir == "" {
			fmt.Print("Enter path to your music directory: ")
			dir, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			dir = strings.TrimSpace(dir)
			if dir == "" {
				return fmt.Errorf("no music directory provided")
			}
			cfg.MusicDir = dir
			if saveErr := cfg.Save(cfgFile); saveErr != nil && debug {
				fmt.Fprintf(os.Stderr, "debug: saving music dir: %v\n", saveErr)
			}
			fmt.Println("Music directory saved. You can change it anytime in Settings.")
		}

		iconPath := assets.InstallIcon()
		assets.InstallDesktopEntry()
		prov, err := localProvider.New(cfg.MusicDir)
		if err != nil {
			return fmt.Errorf("local provider: %w", err)
		}
		plyr, err := localPlayer.New()
		if err != nil {
			return fmt.Errorf("local player: %w", err)
		}
		tracks, err := prov.GetLibraryTracks(context.Background())
		if err != nil {
			return fmt.Errorf("loading local tracks: %w", err)
		}
		plyr.LoadTracks(tracks)
		// Auto-play from the first track on launch.
		if len(tracks) > 0 {
			ids := make([]string, len(tracks))
			for i, t := range tracks {
				ids[i] = t.ID
			}
			_ = plyr.SetQueue(ids)
		}
		opts.IconPath = iconPath
		opts.InitialTracks = tracks
		opts.Backend = "Local mode · playing from " + cfg.MusicDir
		prog := tea.NewProgram(tui.New(cfg, prov, plyr, opts))
		_, err = prog.Run()
		return err
	}

	if demo {
		iconPath := assets.InstallIcon()
		assets.InstallDesktopEntry()
		p := demoPlayer.New()
		dp := demoProvider.Provider{}
		opts.IconPath = iconPath
		opts.Backend = "Demo mode · built-in fake tracks, no credentials required"
		prog := tea.NewProgram(tui.New(cfg, dp, p, opts))
		_, err = prog.Run()
		return err
	}

	auth.ApplyEmbedded(cfg)
	lastfm.ApplyEmbedded(cfg)

	if cfg.AppleDeveloperToken == "" {
		return fmt.Errorf("apple developer token not set.\n\nSet apple_developer_token in ~/.config/vibez/config.json\nor run: go run ./scripts/gen-devtoken")
	}

	// Install icon and .desktop entry on every launch (idempotent, best-effort).
	// The .desktop file has NoDisplay=true so it stays invisible to app launchers
	// but lets the DE resolve the icon via the MPRIS DesktopEntry property.
	iconPath := assets.InstallIcon()
	assets.InstallDesktopEntry()
	opts.IconPath = iconPath

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

	return runPlatform(cfg, iconPath, opts, onUserToken, onStorefront, audioBitrateKbps)

}
