//go:build linux || darwin

package cmd

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/lastfm"
	playerpkg "github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/cdp"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/updater"
	"github.com/simone-vibes/vibez/internal/version"
)

type cdpPlatformHooks struct {
	initStatus  string
	afterReady  func(*cdp.Player)
	helperPaths func() []string
	backend     func() string
}

func runCDPFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), hooks cdpPlatformHooks) error {
	prog := tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *cdp.Player, 1)
	runDone := make(chan struct{})
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
			if !auth.ValidateToken(cfg.AppleDeveloperToken, cfg.AppleUserToken) {
				prog.Send(tui.InitStatusMsg("Session expired - re-authenticating..."))
				cfg.AppleUserToken = ""
				_ = cfg.Save("")
			}
		}

		status := hooks.initStatus
		if status == "" {
			status = "Initializing vibez..."
		}
		prog.Send(tui.InitStatusMsg(status))
		if err := cdp.EnsureBrowser(func(msg string) {
			prog.Send(tui.InitStatusMsg(msg))
		}); err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("browser setup: %w", err)})
			return
		}

		if cfg.AppleUserToken == "" {
			prog.Send(tui.InitStatusMsg("Authorizing with Apple Music..."))
			if err := auth.Login(cfg); err != nil {
				prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
				return
			}
		}

		prog.Send(tui.InitStatusMsg("Starting audio engine..."))
		cdpPlayer, err := cdp.New(cfg.AppleDeveloperToken, cfg.AppleUserToken, cfg.StoreFront, cfg.WSL)
		if err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine: %w", err)})
			return
		}
		cdpPlayer.OnUserToken = onUserToken
		cdpPlayer.OnStorefront = onStorefront
		cdpPlayer.OnSessionExpired = func() {
			prog.Send(tui.SessionExpiredMsg{})
			go func() {
				if err := auth.Login(cfg); err != nil {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("re-auth: %w", err)})
					return
				}
				if err := cdpPlayer.SetUserToken(cfg.AppleUserToken); err != nil {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("re-auth token update: %w", err)})
					return
				}
				cdpPlayer.ResetSessionExpired()
				prog.Send(tui.SessionRestoredMsg{})
			}()
		}

		playerCh <- cdpPlayer
		go func() {
			defer close(runDone)
			cdpPlayer.Run()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if waitErr := cdpPlayer.WaitReady(ctx); waitErr != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine init: %w", waitErr)})
			cdpPlayer.Terminate()
			return
		}

		if hooks.afterReady != nil {
			hooks.afterReady(cdpPlayer)
		}
		startLastfmScrobbler(cfg, cdpPlayer, func(msg string) { prog.Send(tui.DebugLogMsg(msg)) })

		prog.Send(tui.EngineReadyMsg{
			Player:      cdpPlayer,
			Provider:    apple.New(cfg),
			HelperPaths: hooks.helperPaths(),
			Backend:     hooks.backend(),
		})
	}()

	_, err := prog.Run()

	select {
	case p := <-playerCh:
		p.Terminate()
		<-runDone
	default:
	}

	select {
	case exe := <-restartExe:
		_ = syscall.Exec(exe, os.Args, os.Environ()) //nolint:gosec
	default:
	}

	return err
}

func startLastfmScrobbler(cfg *config.Config, player interface {
	Subscribe() <-chan playerpkg.State
}, log func(string)) {
	if cfg.LastfmAPIKey == "" || cfg.LastfmAPISecret == "" || cfg.LastfmSessionKey == "" {
		return
	}
	lfmClient := lastfm.NewClient(cfg.LastfmAPIKey, cfg.LastfmAPISecret, cfg.LastfmSessionKey)
	scrobbler := lastfm.NewScrobbler(lfmClient)
	scrobbler.SetLogger(log)
	go func() {
		for st := range player.Subscribe() {
			scrobbler.Update(st)
		}
	}()
}
