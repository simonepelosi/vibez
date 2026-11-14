//go:build linux || darwin || windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	localProvider "github.com/simone-vibes/vibez/internal/provider/local"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/updater"
	"github.com/simone-vibes/vibez/internal/version"
)

func runBrowserlessFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	prog := tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *browserless.Player, 1)
	restartExe := make(chan string, 1)
	scanCtx, cancelScan := context.WithCancel(context.Background())
	defer cancelScan()
	var stopRPC func()

	go func() {
		var (
			exe        string
			authStatus = auth.TokensValid
			sf         string
			wg         sync.WaitGroup
		)
		wg.Go(func() {
			exe = updater.CheckAndUpdate(version.Version, noUpdate, func(msg string) {
				prog.Send(tui.InitStatusMsg(msg))
			})
		})
		if cfg.AppleUserToken != "" {
			prog.Send(tui.InitStatusMsg("Checking Apple Music session..."))
			wg.Go(func() {
				authStatus, sf = auth.CheckTokens(cfg.AppleDeveloperToken, cfg.AppleUserToken)
			})
		}
		wg.Wait()

		if exe != "" {
			restartExe <- exe
			prog.Send(tui.RestartMsg{})
			return
		}

		devTokenRejected := false
		if cfg.AppleUserToken != "" {
			switch authStatus {
			case auth.UserTokenRejected:
				prog.Send(tui.InitStatusMsg("Session expired - re-authenticating..."))
				cfg.AppleUserToken = ""
				_ = cfg.Save("")
			case auth.DeveloperTokenRejected:
				devTokenRejected = true
				res := updater.UpdateNow(version.Version, noUpdate, func(msg string) {
					prog.Send(tui.InitStatusMsg(msg))
				})
				if res.Outcome == updater.OutcomeInstalled {
					restartExe <- res.Exe
					prog.Send(tui.RestartMsg{})
					return
				}
				statusMsg := auth.DeveloperTokenStatus
				if advice := res.Advice(); advice != "" {
					statusMsg = auth.DeveloperTokenPrefix + " - " + advice
				}
				prog.Send(tui.InitStatusMsg(statusMsg))
			case auth.TokensValid:
				if onStorefront != nil {
					onStorefront(sf)
				}
			}
		}

		tuiAuthLog := func(msg string) {
			prog.Send(tui.InitStatusMsg(msg))
			prog.Send(tui.DebugLogMsg("[auth] " + msg))
		}

		if cfg.AppleUserToken == "" {
			prog.Send(tui.InitStatusMsg("Authorizing with Apple Music..."))
			if err := auth.LoginWithLogger(cfg, tuiAuthLog); err != nil {
				prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
				return
			}
			if onUserToken != nil {
				onUserToken(cfg.AppleUserToken)
			}
			if cfg.StoreFront == "" && onStorefront != nil {
				if _, sf := auth.CheckTokens(cfg.AppleDeveloperToken, cfg.AppleUserToken); sf != "" {
					onStorefront(sf)
				}
			}
		}

		if !devTokenRejected {
			prog.Send(tui.InitStatusMsg("Starting audio engine..."))
		}
		prov := apple.New(cfg)
		blPlayer, err := browserless.New(cfg, prov, func(msg string) {
			if !devTokenRejected {
				prog.Send(tui.InitStatusMsg(msg))
			}
		})
		if err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine: %w", err)})
			return
		}
		_ = blPlayer.SetAudioBitrate(audioBitrateKbps)
		blPlayer.OnSessionExpired = func() {
			prog.Send(tui.SessionExpiredMsg{})
			go func() {
				if err := auth.LoginWithLogger(cfg, tuiAuthLog); err != nil {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("re-auth: %w", err)})
					return
				}
				if onUserToken != nil {
					onUserToken(cfg.AppleUserToken)
				}
				blPlayer.ResetSessionExpired()
				prog.Send(tui.SessionRestoredMsg{})
			}()
		}

		playerCh <- blPlayer

		debugLog := func(msg string) { prog.Send(tui.DebugLogMsg(msg)) }
		setupBrowserlessPlatform(blPlayer, debugLog)

		// Last.fm scrobbler & Discord RPC integration
		startLastfmScrobbler(cfg, blPlayer, debugLog)
		stopRPC = startDiscordRPC(cfg, blPlayer, debugLog)

		backendLabel := browserlessBackendLabel(audioBitrateKbps)
		prog.Send(tui.EngineReadyMsg{
			Player:   blPlayer,
			Provider: prov,
			Backend:  backendLabel,
		})

		if cfg.HybridPlayback && cfg.MusicDir != "" {
			go func() {
				matcher, mErr := localProvider.NewMatcherContext(scanCtx, cfg.MusicDir)
				if scanCtx.Err() != nil {
					return
				}
				if mErr != nil {
					prog.Send(tui.DebugLogMsg(fmt.Sprintf("[hybrid] local scan failed (%s): %v", cfg.MusicDir, mErr)))
					return
				}
				blPlayer.SetLocalMatcher(matcher)
				prog.Send(tui.DebugLogMsg(fmt.Sprintf("[hybrid] indexed %d lossless local track(s) in %s", matcher.Count(), cfg.MusicDir)))
			}()
		}
	}()

	_, err := prog.Run()
	cancelScan()
	if stopRPC != nil {
		stopRPC()
	}

	select {
	case p := <-playerCh:
		_ = p.Close()
	default:
	}

	select {
	case exe := <-restartExe:
		if restartErr := restartProcess(exe); restartErr != nil {
			return errors.Join(err, fmt.Errorf("restarting into the update at %s: %w", exe, restartErr))
		}
	default:
	}

	return err
}
