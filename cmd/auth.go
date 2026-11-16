package cmd

import (
	"fmt"
	"time"

	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to your music provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return auth.Login(cfg)
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out and clear saved tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return auth.Logout(cfg)
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		_ = auth.ResolveDeveloperToken(cfg)
		devTok, usrTok := cfg.GetAppleTokens()
		if exp, ok := auth.DeveloperTokenExpiry(devTok); ok {
			if rem := time.Until(exp).Round(time.Minute); rem > 0 {
				fmt.Printf("Developer token: valid (expires in %s)\n", rem)
			} else {
				fmt.Println("Developer token: expired")
			}
		}
		if usrTok == "" {
			fmt.Println("Not authenticated.")
			return nil
		}
		status, sf := auth.CheckTokens(devTok, usrTok)
		switch status {
		case auth.UserTokenRejected:
			fmt.Println("Not authenticated (session expired or revoked — run 'vibez auth login').")
			return nil
		case auth.DeveloperTokenRejected:
			fmt.Println(auth.DeveloperTokenHelp)
			return nil
		case auth.TokensValid:
			curSF := cfg.Storefront()
			if sf != "" && sf != curSF {
				cfg.SetStorefront(sf)
				_ = cfg.Save(cfgFile)
			}
			if sf == "" {
				sf = curSF
			}
			if sf == "" {
				sf = "auto-detected"
			}
			fmt.Printf("Authenticated with Apple Music (storefront: %s)\n", sf)
			return nil
		}
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
