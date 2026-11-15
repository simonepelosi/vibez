//go:build linux || darwin || windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	localProvider "github.com/simone-vibes/vibez/internal/provider/local"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/updater"
	"github.com/simone-vibes/vibez/internal/version"
)

func runBrowserlessFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	var (
		prog           *tea.Program
		activePlayer   atomic.Pointer[browserless.Player]
		reauthInFlight atomic.Bool
		loggedOut      atomic.Bool
		authGen        atomic.Uint64
		reauthMu       sync.Mutex
		reauthCancel   context.CancelFunc
	)

	tuiAuthLog := func(msg string) {
		if prog != nil {
			prog.Send(tui.InitStatusMsg(msg))
			prog.Send(tui.DebugLogMsg("[auth] " + msg))
		}
	}

	cancelActiveReauth := func() {
		reauthMu.Lock()
		cancel := reauthCancel
		reauthCancel = nil
		reauthMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	handleLogout := func() {
		loggedOut.Store(true)
		authGen.Add(1)
		cancelActiveReauth()
		cfg.SetUserToken("")
	}

	startReauth := func(allowWhenLoggedOut bool) {
		if allowWhenLoggedOut {
			loggedOut.Store(false)
			authGen.Add(1)
		} else if loggedOut.Load() {
			return
		}
		if !reauthInFlight.CompareAndSwap(false, true) {
			return
		}
		if loggedOut.Load() {
			reauthInFlight.Store(false)
			return
		}
		gen := authGen.Load()
		reauthMu.Lock()
		if loggedOut.Load() || authGen.Load() != gen {
			reauthMu.Unlock()
			reauthInFlight.Store(false)
			return
		}
		reauthCtx, cancel := context.WithCancel(context.Background())
		reauthCancel = cancel
		reauthMu.Unlock()

		if prog != nil {
			prog.Send(tui.SessionExpiredMsg{})
		}
		go func() {
			defer func() {
				cancel()
				reauthMu.Lock()
				reauthCancel = nil
				reauthMu.Unlock()
				reauthInFlight.Store(false)
			}()
			if err := auth.LoginWithContext(reauthCtx, cfg, tuiAuthLog); err != nil {
				if !errors.Is(err, auth.ErrLoginCancelled) && prog != nil {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("re-auth: %w", err)})
				}
				return
			}
			if loggedOut.Load() || authGen.Load() != gen {
				cfg.SetUserToken("")
				return
			}
			devTok, usrTok := cfg.GetAppleTokens()
			if onUserToken != nil {
				onUserToken(usrTok)
			}
			if cfg.StoreFront == "" && onStorefront != nil {
				if _, sf := auth.CheckTokens(devTok, usrTok); sf != "" {
					onStorefront(sf)
				}
			}
			if p := activePlayer.Load(); p != nil {
				p.ResetSessionExpired()
			}
			if prog != nil && !loggedOut.Load() && authGen.Load() == gen {
				prog.Send(tui.SessionRestoredMsg{})
			}
		}()
	}

	triggerReauth := func() { startReauth(false) }
	triggerInteractiveLogin := func() { startReauth(true) }

	opts.OnReauth = triggerInteractiveLogin
	opts.OnLogout = handleLogout
	prog = tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *browserless.Player, 1)
	restartExe := make(chan string, 1)
	scanCtx, cancelScan := context.WithCancel(context.Background())
	defer cancelScan()
	defer cancelActiveReauth()
	var stopRPC func()

	go func() {
		devTok, usrTok := cfg.GetAppleTokens()
		var (
			exe               string
			authStatus        = auth.TokensValid
			sf                string
			wg                sync.WaitGroup
			optimisticSession = usrTok != "" && cfg.StoreFront != "" && !auth.IsDeveloperTokenExpired(devTok)
		)
		wg.Go(func() {
			exe = updater.CheckAndUpdate(version.Version, noUpdate, func(msg string) {
				prog.Send(tui.InitStatusMsg(msg))
			})
		})
		if usrTok != "" && !optimisticSession {
			prog.Send(tui.InitStatusMsg("Checking Apple Music session..."))
			wg.Go(func() {
				dTok, uTok := cfg.GetAppleTokens()
				authStatus, sf = auth.CheckTokens(dTok, uTok)
			})
		}
		wg.Wait()

		if exe != "" {
			restartExe <- exe
			prog.Send(tui.RestartMsg{})
			return
		}

		devTokenRejected := false
		if cfg.UserToken() != "" && !optimisticSession {
			switch authStatus {
			case auth.UserTokenRejected:
				prog.Send(tui.InitStatusMsg("Session expired - re-authenticating..."))
				cfg.SetUserToken("")
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

		if cfg.UserToken() == "" && !loggedOut.Load() {
			prog.Send(tui.InitStatusMsg("Authorizing with Apple Music..."))
			reauthMu.Lock()
			loginCtx, cancelLogin := context.WithCancel(context.Background())
			reauthCancel = cancelLogin
			reauthMu.Unlock()
			err := auth.LoginWithContext(loginCtx, cfg, tuiAuthLog)
			cancelLogin()
			reauthMu.Lock()
			reauthCancel = nil
			reauthMu.Unlock()
			if err != nil {
				if !errors.Is(err, auth.ErrLoginCancelled) {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
				}
				return
			}
			dTok, uTok := cfg.GetAppleTokens()
			if onUserToken != nil {
				onUserToken(uTok)
			}
			if cfg.StoreFront == "" && onStorefront != nil {
				if _, sf := auth.CheckTokens(dTok, uTok); sf != "" {
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
		activePlayer.Store(blPlayer)
		blPlayer.OnSessionExpired = triggerReauth

		if optimisticSession {
			go func() {
				if loggedOut.Load() {
					return
				}
				dTok, uTok := cfg.GetAppleTokens()
				if uTok == "" {
					return
				}
				st, freshSF := auth.CheckTokens(dTok, uTok)
				if loggedOut.Load() {
					return
				}
				switch st {
				case auth.TokensValid:
					if onStorefront != nil && freshSF != "" {
						onStorefront(freshSF)
					}
				case auth.UserTokenRejected:
					triggerReauth()
				case auth.DeveloperTokenRejected:
					prog.Send(tui.InitStatusMsg(auth.DeveloperTokenStatus))
				}
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
				cleanDir := provider.Clean(cfg.MusicDir)
				if mErr != nil {
					prog.Send(tui.DebugLogMsg(fmt.Sprintf("[hybrid] local scan failed (%s): %s", cleanDir, provider.Clean(mErr.Error()))))
					return
				}
				blPlayer.SetLocalMatcher(matcher)
				prog.Send(tui.DebugLogMsg(fmt.Sprintf("[hybrid] indexed %d lossless local track(s) in %s", matcher.Count(), cleanDir)))
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
