//go:build linux || darwin || windows

package cmd

import (
	"testing"

	"github.com/simone-vibes/vibez/internal/config"
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
