package auth

import (
	"os"
	"testing"
)

// TestMain redirects platform configuration paths to a throwaway directory.
//
// Several tests exercise Login/Logout, which persist state via
// config.Save("") → ConfigPath(""). Without
// this guard, running `go test ./...` silently overwrites the developer's real
// config with test fixtures (e.g. the bogus "embedded-takes-priority" developer
// token), which then makes `vibez` fail to launch with an "invalid token" error.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "vibez-auth-test-home-*")
	if err != nil {
		panic("creating temp HOME for auth tests: " + err.Error())
	}
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		if err := os.Setenv(key, tmp); err != nil {
			panic("setting temp " + key + " for auth tests: " + err.Error())
		}
	}

	code := m.Run()

	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
