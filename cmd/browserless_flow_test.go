//go:build linux || darwin || windows

package cmd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless"
	localProvider "github.com/simone-vibes/vibez/internal/provider/local"
	"github.com/simone-vibes/vibez/internal/tui"
)

func setIsolatedConfigEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
}

func TestReauthCoordinator_ConcurrentInteractiveLoginIsNoOp(t *testing.T) {
	setIsolatedConfigEnv(t)
	cfg := &config.Config{AppleDeveloperToken: "dev-tok"}
	if err := cfg.Save(""); err != nil {
		t.Fatal(err)
	}

	var committedToken string
	coord := &reauthCoordinator{
		cfg:         cfg,
		onUserToken: func(tok string) { committedToken = tok },
	}

	// Simulate startup login acquiring in-flight ownership.
	gen0, _, cancel0, ok := coord.beginLogin(false, false)
	if !ok {
		t.Fatal("startup beginLogin failed")
	}
	defer coord.finishLogin(cancel0)

	// Simulate user typing :login while startup login is already active.
	if _, _, _, ok := coord.beginLogin(true, true); ok {
		t.Fatal("concurrent interactive beginLogin succeeded while login was already in flight")
	}

	// Startup login finishes and must still commit without generation invalidation.
	cfg.SetUserToken("user-tok-1")
	_, uTok, ok := coord.commitLogin(gen0)
	if !ok || uTok != "user-tok-1" || committedToken != "user-tok-1" {
		t.Fatalf("commitLogin(gen0) = (%q, %v), committed=%q; want user-tok-1, true", uTok, ok, committedToken)
	}
}

func TestReauthCoordinator_LogoutCancelsReauthBeforeCommandRuns(t *testing.T) {
	coord := &reauthCoordinator{}
	runCalled := false
	work := coord.prepareReauth(true, func(context.Context, uint64) { runCalled = true })
	if work == nil {
		t.Fatal("interactive login did not return a command")
	}

	coord.logout()
	_ = work()
	if runCalled {
		t.Fatal("an interactive login queued before :logout ran after :logout")
	}
	if !coord.loggedOut.Load() || coord.reauthInFlight.Load() {
		t.Fatal("cancelled login did not leave the coordinator logged out and idle")
	}
}

func TestReauthCoordinator_LogoutCancelsActiveReauthAndResetsOnSubsequentLogin(t *testing.T) {
	setIsolatedConfigEnv(t)
	cfg := &config.Config{AppleDeveloperToken: "dev-tok", AppleUserToken: "old-tok"}
	if err := cfg.Save(""); err != nil {
		t.Fatal(err)
	}

	coord := &reauthCoordinator{cfg: cfg}

	gen1, ctx1, cancel1, ok := coord.beginLogin(true, true)
	if !ok {
		t.Fatal("initial beginLogin failed")
	}

	coord.logout()
	if ctx1.Err() == nil {
		t.Fatal("logout did not cancel in-flight reauth context")
	}
	coord.finishLogin(cancel1)

	cfg.SetUserToken("late-tok")
	if _, _, ok := coord.commitLogin(gen1); ok {
		t.Fatal("commitLogin(stale gen1) unexpectedly succeeded after logout")
	}
	if cfg.UserToken() != "" {
		t.Fatalf("cfg.UserToken() = %q after fenced commit, want empty", cfg.UserToken())
	}

	// Background auto-reauth must remain refused while logged out.
	if _, _, _, ok := coord.beginLogin(false, false); ok {
		t.Fatal("background beginLogin succeeded while logged out")
	}

	// Subsequent explicit :login resets loggedOut and allows commit.
	gen2, _, cancel2, ok := coord.beginLogin(true, true)
	if !ok {
		t.Fatal("interactive beginLogin after logout failed")
	}
	defer coord.finishLogin(cancel2)

	cfg.SetUserToken("fresh-tok")
	if _, uTok, ok := coord.commitLogin(gen2); !ok || uTok != "fresh-tok" {
		t.Fatalf("commitLogin(gen2) = (%q, %v), want (fresh-tok, true)", uTok, ok)
	}
}

func TestReauthCoordinator_StartReauth_InteractiveSkipsSessionExpiredNotification(t *testing.T) {
	setIsolatedConfigEnv(t)
	cfg := &config.Config{AppleDeveloperToken: "dev-tok"}
	if err := cfg.Save(""); err != nil {
		t.Fatal(err)
	}

	coord := &reauthCoordinator{cfg: cfg}

	// Interactive :login should not fire the background "session expired" banner.
	expiredCount := 0
	successTok := ""
	work := coord.startReauth(true, reauthHooks{
		onExpired: func() { expiredCount++ },
		login: func(context.Context) error {
			cfg.SetUserToken("interactive-tok")
			return nil
		},
		onSuccess: func(_, usrTok string, _ uint64) { successTok = usrTok },
		onErr:     func(err error) { t.Fatalf("unexpected onErr: %v", err) },
	})
	if work == nil {
		t.Fatal("interactive startReauth returned nil cmd")
	}
	_ = work()
	if expiredCount != 0 {
		t.Fatalf("interactive login triggered onExpired %d times, want 0", expiredCount)
	}
	if successTok != "interactive-tok" {
		t.Fatalf("interactive onSuccess token = %q, want interactive-tok", successTok)
	}

	// Background session-expiry re-auth must still emit the notice.
	expiredCount = 0
	successTok = ""
	bgWork := coord.startReauth(false, reauthHooks{
		onExpired: func() { expiredCount++ },
		login: func(context.Context) error {
			cfg.SetUserToken("bg-tok")
			return nil
		},
		onSuccess: func(_, usrTok string, _ uint64) { successTok = usrTok },
		onErr:     func(err error) { t.Fatalf("unexpected onErr: %v", err) },
	})
	if bgWork == nil {
		t.Fatal("background startReauth returned nil cmd")
	}
	_ = bgWork()
	if expiredCount != 1 {
		t.Fatalf("background re-auth triggered onExpired %d times, want 1", expiredCount)
	}
	if successTok != "bg-tok" {
		t.Fatalf("background onSuccess token = %q, want bg-tok", successTok)
	}
}

func TestReauthCoordinator_StartReauth_CancelledOrLogoutSkipsNotification(t *testing.T) {
	setIsolatedConfigEnv(t)
	cfg := &config.Config{AppleDeveloperToken: "dev-tok"}
	coord := &reauthCoordinator{cfg: cfg}

	expiredCalled := false
	loginCalled := false
	errCalled := false
	work := coord.startReauth(false, reauthHooks{
		onExpired: func() { expiredCalled = true },
		login: func(ctx context.Context) error {
			loginCalled = true
			return auth.ErrLoginCancelled
		},
		onErr: func(error) { errCalled = true },
	})
	if work == nil {
		t.Fatal("startReauth returned nil cmd")
	}

	coord.logout()
	_ = work()
	if expiredCalled || loginCalled || errCalled {
		t.Fatalf("cancelled re-auth invoked callbacks: expired=%v login=%v err=%v", expiredCalled, loginCalled, errCalled)
	}

	// Even if cancellation happens synchronously right before login returns ErrLoginCancelled,
	// cancelled errors must not trigger onErr.
	coord.loggedOut.Store(false)
	errCalled = false
	work2 := coord.startReauth(false, reauthHooks{
		onExpired: func() { expiredCalled = true },
		login: func(ctx context.Context) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			coord.logout()
			return errors.Join(auth.ErrLoginCancelled, context.Canceled)
		},
		onSuccess: func(_, _ string, _ uint64) { t.Fatal("onSuccess called after logout") },
		onErr:     func(err error) { errCalled = true },
	})
	if work2 == nil {
		t.Fatal("second startReauth returned nil cmd")
	}
	_ = work2()
	if !expiredCalled {
		t.Fatal("expected background re-auth to notify onExpired before logout")
	}
	if errCalled {
		t.Fatal("cancelled re-auth reported ErrLoginCancelled to onErr")
	}
}

func TestHybridCoordinator_MissingDirAndDisabledMode(t *testing.T) {
	var (
		mu   sync.Mutex
		msgs []tea.Msg
	)
	record := func(msg tea.Msg) {
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
	}

	disabled := &hybridCoordinator{
		cfg:     &config.Config{HybridPlayback: false, MusicDir: "/music"},
		sendMsg: record,
	}
	if started, msg := disabled.triggerScan(); started || !strings.Contains(msg, "--hybrid") {
		t.Fatalf("disabled triggerScan() = (%v, %q), want (false, --hybrid hint)", started, msg)
	}

	missingDir := &hybridCoordinator{
		cfg:     &config.Config{HybridPlayback: true, MusicDir: ""},
		sendMsg: record,
	}
	missingDir.startStartupScan()

	mu.Lock()
	defer mu.Unlock()
	sawStatus := false
	sawLog := false
	for _, m := range msgs {
		switch v := m.(type) {
		case tui.HybridStatusMsg:
			if v.Enabled && v.Err == "no dir" && !v.Scanning {
				sawStatus = true
			}
		case tui.DebugLogMsg:
			if strings.Contains(string(v), "music_dir is not set") {
				sawLog = true
			}
		}
	}
	if !sawStatus || !sawLog {
		t.Fatalf("missing dir emitted msgs=%+v, want HybridStatusMsg{Err: no dir} and warning DebugLogMsg", msgs)
	}
}

func TestHybridCoordinator_ProgressLogsConcurrentGuardAndErrorPreservation(t *testing.T) {
	var (
		mu        sync.Mutex
		msgs      []tea.Msg
		pPtr      atomic.Pointer[browserless.Player]
		scanNum   atomic.Int32
		started   = make(chan struct{})
		release   = make(chan struct{})
		wantDir   = t.TempDir()
		emptyM, _ = localProvider.NewMatcher(wantDir)
	)
	p := &browserless.Player{}
	pPtr.Store(p)

	coord := &hybridCoordinator{
		cfg:          &config.Config{HybridPlayback: true, MusicDir: wantDir},
		scanCtx:      context.Background(),
		activePlayer: &pPtr,
		sendMsg: func(msg tea.Msg) {
			mu.Lock()
			msgs = append(msgs, msg)
			mu.Unlock()
		},
		scanFn: func(_ context.Context, _ string, onProgress func(localProvider.ScanProgress)) (*localProvider.Matcher, error) {
			n := scanNum.Add(1)
			if n == 1 {
				close(started)
				<-release
				onProgress(localProvider.ScanProgress{Phase: localProvider.ScanPhaseWalk, Walked: 120, Found: 10})
				onProgress(localProvider.ScanProgress{Phase: localProvider.ScanPhaseParseStart, Found: 10, Cached: 2, TotalToParse: 8})
				onProgress(localProvider.ScanProgress{Phase: localProvider.ScanPhaseParse, Found: 10, Cached: 2, Parsed: 4, TotalToParse: 8})
				return emptyM, nil
			}
			return nil, errors.New("nas offline")
		},
	}

	ok, banner := coord.triggerScan()
	if !ok || banner == "" {
		t.Fatalf("initial triggerScan() = (%v, %q), want (true, non-empty)", ok, banner)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scanFn did not start in time")
	}

	// Second concurrent scan must be rejected without interrupting the active scan.
	if ok2, msg2 := coord.triggerScan(); ok2 || !strings.Contains(msg2, "already in progress") {
		close(release)
		coord.closeAndWait()
		t.Fatalf("concurrent triggerScan() = (%v, %q), want already-in-progress rejection", ok2, msg2)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for coord.inFlight.Load() {
		if time.Now().After(deadline) {
			coord.closeAndWait()
			t.Fatal("timed out waiting for first scan to finish")
		}
		time.Sleep(time.Millisecond)
	}
	coord.mu.Lock()
	coord.lastCount = 77
	coord.mu.Unlock()

	// Next scan fails and preserves last known track count with Err="scan failed".
	ok3, _ := coord.triggerScan()
	if !ok3 {
		t.Fatal("post-completion triggerScan failed")
	}
	coord.closeAndWait()

	mu.Lock()
	defer mu.Unlock()
	var logs []string
	var statuses []tui.HybridStatusMsg
	for _, m := range msgs {
		switch v := m.(type) {
		case tui.DebugLogMsg:
			logs = append(logs, string(v))
		case tui.HybridStatusMsg:
			statuses = append(statuses, v)
		}
	}

	for _, wantSubstr := range []string{
		"walked 120 entries, 10 lossless file(s) found",
		"(2 cached, scanning tags for 8 new/changed)",
		"scanning tags: 4/8 (50%)",
		"indexed 0 lossless local track(s)",
		"local scan failed",
	} {
		found := slices.ContainsFunc(logs, func(s string) bool {
			return strings.Contains(s, wantSubstr)
		})
		if !found {
			t.Fatalf("expected log containing %q in %v", wantSubstr, logs)
		}
	}

	lastStatus := statuses[len(statuses)-1]
	if lastStatus.Err != "scan failed" || lastStatus.Count != 77 || lastStatus.Scanning {
		t.Fatalf("final status after error = %+v, want Count=77 Err=scan failed Scanning=false", lastStatus)
	}
}

func TestHybridCoordinator_AttachPlayerAppliesCompletedMatcher(t *testing.T) {
	wantDir := t.TempDir()
	m, err := localProvider.NewMatcher(wantDir)
	if err != nil {
		t.Fatal(err)
	}

	var pPtr atomic.Pointer[browserless.Player]
	coord := &hybridCoordinator{
		cfg:          &config.Config{HybridPlayback: true, MusicDir: wantDir},
		scanCtx:      context.Background(),
		activePlayer: &pPtr,
		scanFn: func(_ context.Context, _ string, _ func(localProvider.ScanProgress)) (*localProvider.Matcher, error) {
			return m, nil
		},
	}

	ok, _ := coord.triggerScan()
	if !ok {
		t.Fatal("triggerScan returned false before player attach")
	}
	coord.closeAndWait()

	p := &browserless.Player{}
	coord.attachPlayer(p)
	if pPtr.Load() != p {
		t.Fatal("attachPlayer did not store active player")
	}
}
