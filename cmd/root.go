package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/assets"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/lastfm"
	demoPlayer "github.com/simone-vibes/vibez/internal/player/demo"
	demoProvider "github.com/simone-vibes/vibez/internal/provider/demo"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/tui/styles"
	"github.com/spf13/cobra"
)

var cfgFile string
var debug bool
var memProfiling bool
var demo bool
var noUpdate bool

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
