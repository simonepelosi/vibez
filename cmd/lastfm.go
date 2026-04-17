package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/lastfm"
	"github.com/spf13/cobra"
)

var lastfmCmd = &cobra.Command{
	Use:   "lastfm",
	Short: "Manage Last.fm scrobbling",
}

var lastfmLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Connect your Last.fm account to enable scrobbling",
	Long: `Authenticate with Last.fm using the application credentials embedded in
vibez. Your play history will be scrobbled automatically during playback.

The session key is stored in ~/.config/vibez/config.json.
If you built vibez from source without embedded keys, set lastfm_api_key and
lastfm_api_secret in that file first (obtain them from https://www.last.fm/api/account/create).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		lastfm.ApplyEmbedded(cfg)

		if cfg.LastfmAPIKey == "" || cfg.LastfmAPISecret == "" {
			return fmt.Errorf(`last.fm API credentials are not available.

If you built vibez from source, register an application at
  https://www.last.fm/api/account/create
and add to ~/.config/vibez/config.json:
  "lastfm_api_key":    "<your API key>",
  "lastfm_api_secret": "<your shared secret>"`)
		}

		client := lastfm.NewClient(cfg.LastfmAPIKey, cfg.LastfmAPISecret, "")

		token, err := client.GetToken()
		if err != nil {
			return fmt.Errorf("requesting Last.fm token: %w", err)
		}

		authURL := client.AuthorizeURL(token)
		fmt.Println("Connecting to Last.fm…")
		_ = exec.Command("xdg-open", authURL).Start() //nolint:gosec // opens a known last.fm URL in the browser

		fmt.Printf("\nIf your browser did not open, visit:\n  %s\n\n", authURL)
		fmt.Print("Press Enter after you have granted access in your browser: ")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')

		sessionKey, err := client.GetSession(token)
		if err != nil {
			return fmt.Errorf("getting Last.fm session: %w", err)
		}

		cfg.LastfmSessionKey = sessionKey
		if err := cfg.Save(cfgFile); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Println("✓ Last.fm connected! Vibez will scrobble your plays automatically.")
		return nil
	},
}

var lastfmLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Disconnect Last.fm and disable scrobbling",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		cfg.LastfmSessionKey = ""
		if err := cfg.Save(cfgFile); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Println("Last.fm disconnected. Scrobbling disabled.")
		return nil
	},
}

var lastfmStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Last.fm connection status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		lastfm.ApplyEmbedded(cfg)

		switch {
		case cfg.LastfmAPIKey == "":
			fmt.Println("Last.fm: not configured (no API key).")
			fmt.Println("Run 'vibez auth lastfm login' to connect your account.")
		case cfg.LastfmSessionKey == "":
			fmt.Println("Last.fm: API credentials available, but not authenticated.")
			fmt.Println("Run 'vibez auth lastfm login' to connect your account.")
		default:
			fmt.Println("Last.fm: connected. Scrobbling is enabled.")
		}
		return nil
	},
}

func init() {
	lastfmCmd.AddCommand(lastfmLoginCmd, lastfmLogoutCmd, lastfmStatusCmd)
	authCmd.AddCommand(lastfmCmd)
}
