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

type reauthCoordinator struct {
	cfg            *config.Config
	onUserToken    func(string)
	reauthInFlight atomic.Bool
	loggedOut      atomic.Bool
	authGen        atomic.Uint64
	reauthMu       sync.Mutex
	reauthCancel   context.CancelFunc
	authCommitMu   sync.Mutex
}

func (c *reauthCoordinator) cancelActive() {
	c.reauthMu.Lock()
	cancel := c.reauthCancel
	c.reauthCancel = nil
	c.reauthMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *reauthCoordinator) logout() {
	c.authCommitMu.Lock()
	c.loggedOut.Store(true)
	c.authGen.Add(1)
	c.cancelActive()
	if c.cfg != nil {
		c.cfg.SetUserToken("")
		_ = c.cfg.Save("")
	}
	c.authCommitMu.Unlock()
}

func (c *reauthCoordinator) commitLogin(gen uint64) (string, string, bool) {
	c.authCommitMu.Lock()
	defer c.authCommitMu.Unlock()
	if c.loggedOut.Load() || c.authGen.Load() != gen {
		if c.cfg != nil {
			c.cfg.SetUserToken("")
			_ = c.cfg.Save("")
		}
		return "", "", false
	}
	devTok, usrTok := c.cfg.GetAppleTokens()
	if c.onUserToken != nil {
		c.onUserToken(usrTok)
	}
	return devTok, usrTok, true
}

func (c *reauthCoordinator) beginLogin(allowWhenLoggedOut, bumpGen bool) (uint64, context.Context, context.CancelFunc, bool) {
	if !allowWhenLoggedOut && c.loggedOut.Load() {
		return 0, nil, nil, false
	}
	// Claim in-flight ownership before mutating loggedOut/authGen so a
	// duplicate :login during an active login is a clean no-op and never
	// invalidates the active login's generation.
	if !c.reauthInFlight.CompareAndSwap(false, true) {
		return 0, nil, nil, false
	}

	c.authCommitMu.Lock()
	if !allowWhenLoggedOut && c.loggedOut.Load() {
		c.authCommitMu.Unlock()
		c.reauthInFlight.Store(false)
		return 0, nil, nil, false
	}
	if allowWhenLoggedOut {
		c.loggedOut.Store(false)
	}
	if bumpGen {
		c.authGen.Add(1)
	}
	gen := c.authGen.Load()
	ctx, cancel := context.WithCancel(context.Background())
	c.reauthMu.Lock()
	c.reauthCancel = cancel
	c.reauthMu.Unlock()
	c.authCommitMu.Unlock()
	return gen, ctx, cancel, true
}

func (c *reauthCoordinator) finishLogin(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	c.reauthMu.Lock()
	c.reauthCancel = nil
	c.reauthMu.Unlock()
	c.reauthInFlight.Store(false)
}

func runBrowserlessFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	var (
		prog         *tea.Program
		activePlayer atomic.Pointer[browserless.Player]
	)
	authState := &reauthCoordinator{
		cfg:         cfg,
		onUserToken: onUserToken,
	}

	tuiAuthLog := func(msg string) {
		if prog != nil {
			prog.Send(tui.InitStatusMsg(msg))
			prog.Send(tui.DebugLogMsg("[auth] " + msg))
		}
	}

	startReauth := func(allowWhenLoggedOut bool) {
		gen, reauthCtx, cancel, ok := authState.beginLogin(allowWhenLoggedOut, allowWhenLoggedOut)
		if !ok {
			return
		}

		if prog != nil {
			prog.Send(tui.SessionExpiredMsg{})
		}
		go func() {
			defer authState.finishLogin(cancel)
			if err := auth.LoginWithContext(reauthCtx, cfg, tuiAuthLog); err != nil {
				if !errors.Is(err, auth.ErrLoginCancelled) && prog != nil {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("re-auth: %w", err)})
				}
				return
			}
			devTok, usrTok, ok := authState.commitLogin(gen)
			if !ok {
				return
			}
			if onStorefront != nil {
				if _, sf := auth.CheckTokens(devTok, usrTok); sf != "" {
					onStorefront(sf)
				}
			}
			if p := activePlayer.Load(); p != nil {
				p.ResetSessionExpired()
			}
			if prog != nil && !authState.loggedOut.Load() && authState.authGen.Load() == gen {
				prog.Send(tui.SessionRestoredMsg{})
			}
		}()
	}

	triggerReauth := func() { startReauth(false) }
	triggerInteractiveLogin := func() { startReauth(true) }

	opts.OnReauth = triggerInteractiveLogin
	opts.OnLogout = authState.logout
	prog = tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *browserless.Player, 1)
	restartExe := make(chan string, 1)
	scanCtx, cancelScan := context.WithCancel(context.Background())
	defer cancelScan()
	defer authState.cancelActive()
	var stopRPC func()

	go func() {
		devTok, usrTok := cfg.GetAppleTokens()
		initialStorefront := cfg.StoreFront
		var (
			exe               string
			authStatus        = auth.TokensValid
			sf                string
			wg                sync.WaitGroup
			optimisticSession = usrTok != "" && initialStorefront != "" && !auth.IsDeveloperTokenExpired(devTok)
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

		if cfg.UserToken() == "" && !authState.loggedOut.Load() {
			if gen, loginCtx, cancelLogin, ok := authState.beginLogin(false, false); ok {
				prog.Send(tui.InitStatusMsg("Authorizing with Apple Music..."))
				err := auth.LoginWithContext(loginCtx, cfg, tuiAuthLog)
				authState.finishLogin(cancelLogin)
				if err != nil {
					if !errors.Is(err, auth.ErrLoginCancelled) {
						prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
					}
					return
				}
				dTok, uTok, ok := authState.commitLogin(gen)
				if !ok {
					return
				}
				if initialStorefront == "" && onStorefront != nil {
					if _, sf := auth.CheckTokens(dTok, uTok); sf != "" {
						onStorefront(sf)
					}
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
				if authState.loggedOut.Load() {
					return
				}
				dTok, uTok := cfg.GetAppleTokens()
				if uTok == "" {
					return
				}
				st, freshSF := auth.CheckTokens(dTok, uTok)
				if authState.loggedOut.Load() {
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
