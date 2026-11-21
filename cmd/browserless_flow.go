//go:build linux || darwin || windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

type reauthHooks struct {
	onExpired func()
	login     func(context.Context) error
	onSuccess func(devTok, usrTok string, gen uint64)
	onErr     func(error)
}

func (c *reauthCoordinator) prepareReauth(interactive bool, run func(context.Context, uint64)) tea.Cmd {
	// Register before returning to the TUI so a subsequent :logout can cancel
	// the attempt even if Bubble Tea hasn't started its command yet.
	gen, ctx, cancel, ok := c.beginLogin(interactive, interactive)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		defer c.finishLogin(cancel)
		if ctx.Err() == nil {
			run(ctx, gen)
		}
		return nil
	}
}

// startReauth schedules an auth attempt and suppresses the "session expired"
// notice on interactive :login/:auth so it does not overwrite the user's
// login banner or race ahead of a cancellation check.
func (c *reauthCoordinator) startReauth(allowWhenLoggedOut bool, hooks reauthHooks) tea.Cmd {
	return c.prepareReauth(allowWhenLoggedOut, func(reauthCtx context.Context, gen uint64) {
		if reauthCtx.Err() != nil {
			return
		}
		if !allowWhenLoggedOut && hooks.onExpired != nil {
			hooks.onExpired()
		}
		if err := hooks.login(reauthCtx); err != nil {
			if !errors.Is(err, auth.ErrLoginCancelled) && hooks.onErr != nil {
				hooks.onErr(err)
			}
			return
		}
		devTok, usrTok, ok := c.commitLogin(gen)
		if !ok {
			return
		}
		if hooks.onSuccess != nil {
			hooks.onSuccess(devTok, usrTok, gen)
		}
	})
}

type hybridCoordinator struct {
	cfg          *config.Config
	scanCtx      context.Context
	activePlayer *atomic.Pointer[browserless.Player]
	sendMsg      func(tea.Msg)
	scanFn       func(context.Context, string, func(localProvider.ScanProgress)) (*localProvider.Matcher, error)

	inFlight atomic.Bool

	mu            sync.Mutex
	closed        bool
	hasMatcher    bool
	latestMatcher *localProvider.Matcher
	lastCount     int
	wg            sync.WaitGroup
}

func (c *hybridCoordinator) emit(msg tea.Msg) {
	if c.sendMsg == nil {
		return
	}
	if c.scanCtx != nil && c.scanCtx.Err() != nil {
		return
	}
	c.sendMsg(msg)
}

func (c *hybridCoordinator) attachPlayer(p *browserless.Player) {
	if p == nil {
		return
	}
	c.mu.Lock()
	if c.activePlayer != nil {
		c.activePlayer.Store(p)
	}
	if c.hasMatcher {
		p.SetLocalMatcher(c.latestMatcher)
	}
	c.mu.Unlock()
}

func (c *hybridCoordinator) closeAndWait() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.wg.Wait()
}

func (c *hybridCoordinator) startStartupScan() {
	if c.cfg == nil || !c.cfg.HybridPlaybackEnabled() {
		return
	}
	if strings.TrimSpace(c.cfg.GetMusicDir()) == "" {
		c.emit(tui.HybridStatusMsg{Enabled: true, Err: "no dir"})
		c.emit(tui.DebugLogMsg("[hybrid] enabled, but music_dir is not set (use --music-dir <path> or set music_dir in config.json)"))
		return
	}
	_, _ = c.triggerScan()
}

func (c *hybridCoordinator) triggerScan() (bool, string) {
	if c.cfg == nil || !c.cfg.HybridPlaybackEnabled() {
		return false, "hybrid playback is not enabled (start with --hybrid --music-dir <path>)"
	}
	dir := strings.TrimSpace(c.cfg.GetMusicDir())
	if dir == "" {
		// Never call c.emit (prog.Send) synchronously here: Options.OnRescan runs
		// inside Bubble Tea's Update loop and would deadlock waiting on itself.
		return false, "music_dir is not set (use --music-dir <path> or set music_dir in config.json)"
	}
	if c.scanCtx != nil && c.scanCtx.Err() != nil {
		return false, "scan cancelled"
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false, "scan cancelled"
	}
	if !c.inFlight.CompareAndSwap(false, true) {
		c.mu.Unlock()
		return false, "local library scan already in progress"
	}
	prevCount := c.lastCount
	c.wg.Add(1)
	c.mu.Unlock()

	cleanDir := provider.Clean(dir)
	scanFn := c.scanFn
	if scanFn == nil {
		scanFn = localProvider.NewMatcherProgress
	}
	ctx := c.scanCtx
	if ctx == nil {
		ctx = context.Background()
	}

	go func() {
		defer c.wg.Done()
		defer c.inFlight.Store(false)

		// Emit status/log off Bubble Tea's event loop so interactive :rescan never deadlocks prog.Send.
		c.emit(tui.HybridStatusMsg{Enabled: true, Scanning: true, Count: prevCount})
		c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] scanning %s…", cleanDir)))

		matcher, mErr := scanFn(ctx, dir, func(p localProvider.ScanProgress) {
			c.emitProgressLog(cleanDir, p)
		})
		if ctx.Err() != nil {
			return
		}
		if mErr != nil {
			c.mu.Lock()
			keepCount := c.lastCount
			c.mu.Unlock()
			c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] local scan failed (%s): %s", cleanDir, provider.Clean(mErr.Error()))))
			c.emit(tui.HybridStatusMsg{Enabled: true, Count: keepCount, Err: "scan failed"})
			return
		}

		count := 0
		if matcher != nil {
			count = matcher.Count()
		}
		c.mu.Lock()
		c.hasMatcher = true
		c.latestMatcher = matcher
		c.lastCount = count
		if c.activePlayer != nil {
			if p := c.activePlayer.Load(); p != nil {
				p.SetLocalMatcher(matcher)
			}
		}
		c.mu.Unlock()
		c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] indexed %d lossless local track(s) in %s", count, cleanDir)))
		c.emit(tui.HybridStatusMsg{Enabled: true, Count: count})
	}()
	return true, "Rescanning local library in background…"
}

func (c *hybridCoordinator) emitProgressLog(cleanDir string, p localProvider.ScanProgress) {
	switch p.Phase {
	case localProvider.ScanPhaseWalk:
		c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] scanning %s: walked %d entries, %d lossless file(s) found…", cleanDir, p.Walked, p.Found)))
	case localProvider.ScanPhaseParseStart:
		if p.TotalToParse == 0 {
			if p.Found > 0 {
				c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] found %d lossless file(s) in %s (%d cached)", p.Found, cleanDir, p.Cached)))
			}
			return
		}
		c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] found %d lossless file(s) in %s (%d cached, scanning tags for %d new/changed)…", p.Found, cleanDir, p.Cached, p.TotalToParse)))
	case localProvider.ScanPhaseParse:
		pct := 0
		if p.TotalToParse > 0 {
			pct = min(100, (p.Parsed*100)/p.TotalToParse)
		}
		c.emit(tui.DebugLogMsg(fmt.Sprintf("[hybrid] scanning tags: %d/%d (%d%%)…", p.Parsed, p.TotalToParse, pct)))
	}
}

func runBrowserlessFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	var (
		prog         *tea.Program
		activePlayer atomic.Pointer[browserless.Player]
	)
	scanCtx, cancelScan := context.WithCancel(context.Background())
	hybridState := &hybridCoordinator{
		cfg:          cfg,
		scanCtx:      scanCtx,
		activePlayer: &activePlayer,
		sendMsg: func(msg tea.Msg) {
			if prog != nil {
				prog.Send(msg)
			}
		},
	}
	authState := &reauthCoordinator{
		cfg:         cfg,
		onUserToken: onUserToken,
	}

	tuiAuthLog := func(msg string) {
		if prog != nil && !authState.loggedOut.Load() {
			prog.Send(tui.InitStatusMsg(msg))
			prog.Send(tui.DebugLogMsg("[auth] " + msg))
		}
	}

	startReauth := func(allowWhenLoggedOut bool) tea.Cmd {
		return authState.startReauth(allowWhenLoggedOut, reauthHooks{
			onExpired: func() {
				// Program.Send must run off Bubble Tea's event loop.
				if prog != nil {
					prog.Send(tui.SessionExpiredMsg{})
				}
			},
			login: func(reauthCtx context.Context) error {
				return auth.LoginWithContext(reauthCtx, cfg, tuiAuthLog)
			},
			onSuccess: func(devTok, usrTok string, gen uint64) {
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
			},
			onErr: func(err error) {
				if prog != nil {
					prog.Send(tui.InitErrMsg{Err: fmt.Errorf("re-auth: %w", err)})
				}
			},
		})
	}

	triggerReauth := func() {
		if work := startReauth(false); work != nil {
			go work()
		}
	}

	opts.OnReauth = func() tea.Cmd { return startReauth(true) }
	opts.OnLogout = authState.logout
	opts.HybridEnabled = cfg.HybridPlaybackEnabled()
	opts.HybridDir = cfg.GetMusicDir()
	opts.OnRescan = hybridState.triggerScan
	prog = tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *browserless.Player, 1)
	restartExe := make(chan string, 1)
	defer hybridState.closeAndWait()
	defer cancelScan()
	defer authState.cancelActive()
	var stopRPC func()

	go func() {
		hybridState.startStartupScan()

		devTok, usrTok := cfg.GetAppleTokens()
		initialStorefront := cfg.Storefront()
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
		hybridState.attachPlayer(blPlayer)
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
	}()

	_, err := prog.Run()
	cancelScan()
	hybridState.closeAndWait()
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
