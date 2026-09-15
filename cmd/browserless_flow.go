//go:build linux || darwin

package cmd

import (
	"fmt"
	"os"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/updater"
	"github.com/simone-vibes/vibez/internal/version"
)

func runBrowserlessFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	prog := tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *browserless.Player, 1)
	restartExe := make(chan string, 1)

	go func() {
		if exe := updater.CheckAndUpdate(version.Version, noUpdate, func(msg string) {
			prog.Send(tui.InitStatusMsg(msg))
		}); exe != "" {
			restartExe <- exe
			prog.Send(tui.RestartMsg{})
			return
		}

		if cfg.AppleUserToken != "" {
			prog.Send(tui.InitStatusMsg("Checking Apple Music session..."))
			switch auth.CheckTokens(cfg.AppleDeveloperToken, cfg.AppleUserToken) {
			case auth.UserTokenRejected:
				prog.Send(tui.InitStatusMsg("Session expired - re-authenticating..."))
				cfg.AppleUserToken = ""
				_ = cfg.Save("")
			case auth.DeveloperTokenRejected:
				prog.Send(tui.InitStatusMsg(auth.DeveloperTokenStatus))
			case auth.TokensValid:
			}
		}

		if cfg.AppleUserToken == "" {
			prog.Send(tui.InitStatusMsg("Authorizing with Apple Music..."))
			if err := auth.Login(cfg); err != nil {
				prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
				return
			}
			if onUserToken != nil {
				onUserToken(cfg.AppleUserToken)
			}
		}

		if browserless.FindCDM() == "" {
			prog.Send(tui.InitStatusMsg("Downloading Widevine CDM component..."))
		} else {
			prog.Send(tui.InitStatusMsg("Starting browserless audio engine..."))
		}
		prov := apple.New(cfg)
		blPlayer, err := browserless.New(cfg, prov)
		if err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine: %w", err)})
			return
		}

		playerCh <- blPlayer

		setupBrowserlessPlatform(blPlayer)

		// Last.fm scrobbler integration
		startLastfmScrobbler(cfg, blPlayer, func(msg string) { prog.Send(tui.DebugLogMsg(msg)) })

		backendLabel := browserlessBackendLabel(audioBitrateKbps)
		prog.Send(tui.EngineReadyMsg{
			Player:   blPlayer,
			Provider: prov,
			Backend:  backendLabel,
		})
	}()

	_, err := prog.Run()

	select {
	case p := <-playerCh:
		_ = p.Close()
	default:
	}

	select {
	case exe := <-restartExe:
		_ = syscall.Exec(exe, os.Args, os.Environ()) //nolint:gosec
	default:
	}

	return err
}
