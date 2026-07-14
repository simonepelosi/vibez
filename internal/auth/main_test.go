package auth

import (
	"os"
	"testing"
)

// TestMain redirects $HOME to a throwaway directory for the entire auth test
// package.
//
// Several tests exercise Login/Logout, which persist state via
// config.Save("") → ConfigPath("") → $HOME/.config/vibez/config.json. Without
// this guard, running `go test ./...` silently overwrites the developer's real
// config with test fixtures (e.g. the bogus "embedded-takes-priority" developer
// token), which then makes `vibez` fail to launch with an "invalid token" error.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "vibez-auth-test-home-*")
	if err != nil {
		panic("creating temp HOME for auth tests: " + err.Error())
	}
	// os.UserHomeDir() (used by config.ConfigPath) reads $HOME on Linux and
	// macOS, so overriding it keeps every config.Save("") inside tmp.
	if err := os.Setenv("HOME", tmp); err != nil {
		panic("setting temp HOME for auth tests: " + err.Error())
	}

	code := m.Run()

	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
