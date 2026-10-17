//go:build linux

package cdp

import (
	"slices"
	"strings"
	"testing"
)

func TestLaunchArgs_WSL(t *testing.T) {
	args := launchArgs("/tmp/widevine", false, true, false)

	if !slices.Contains(args, "--audio-buffer-size=4096") {
		t.Fatal("wsl=true: missing --audio-buffer-size=4096")
	}
	disableFeaturesContains(t, args, "AudioServiceOutOfProcess", true)
	if !slices.Contains(args, "--widevine-path=/tmp/widevine") {
		t.Fatal("missing --widevine-path")
	}
	if slices.Contains(args, "--single-process") {
		t.Fatal("--single-process must not be present")
	}
	if slices.Contains(args, "--ignore-certificate-errors") {
		t.Fatal("--ignore-certificate-errors must not be present")
	}
}

func TestLaunchArgs_NonWSL(t *testing.T) {
	args := launchArgs("/tmp/widevine", false, false, false)

	if slices.Contains(args, "--audio-buffer-size=4096") {
		t.Fatal("wsl=false: --audio-buffer-size=4096 should not be present")
	}
	disableFeaturesContains(t, args, "AudioServiceOutOfProcess", false)
	if !slices.Contains(args, "--widevine-path=/tmp/widevine") {
		t.Fatal("missing --widevine-path")
	}
}

// disableFeaturesContains checks whether a feature name appears inside the
// --disable-features=... argument in args.
func disableFeaturesContains(t *testing.T, args []string, feature string, want bool) {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, "--disable-features=") {
			got := strings.Contains(a, feature)
			if got != want {
				if want {
					t.Fatalf("--disable-features missing %q", feature)
				} else {
					t.Fatalf("--disable-features should not contain %q", feature)
				}
			}
			return
		}
	}
	t.Fatal("--disable-features flag not found")
}

func TestLaunchArgs_SandboxOn(t *testing.T) {
	args := launchArgs("/tmp/widevine", false, false, true)
	for _, forbidden := range []string{"--no-sandbox", "--disable-setuid-sandbox", "--no-zygote"} {
		if slices.Contains(args, forbidden) {
			t.Errorf("sandboxed launch must not pass %s", forbidden)
		}
	}
}

func TestLaunchArgs_SandboxOff(t *testing.T) {
	args := launchArgs("/tmp/widevine", false, false, false)
	for _, want := range []string{"--no-sandbox", "--disable-setuid-sandbox", "--no-zygote"} {
		if !slices.Contains(args, want) {
			t.Errorf("unsandboxed launch is missing %s", want)
		}
	}
}

func TestSandboxPossible(t *testing.T) {
	vals := func(m map[string]string) func(string) string {
		return func(p string) string { return m[p] }
	}
	cases := []struct {
		name    string
		euid    int
		flatpak bool
		proc    map[string]string
		want    bool
	}{
		{"plain desktop", 1000, false, map[string]string{}, true},
		{"root", 0, false, map[string]string{}, false},
		{"flatpak", 1000, true, map[string]string{}, false},
		{"userns disabled", 1000, false, map[string]string{"/proc/sys/user/max_user_namespaces": "0"}, false},
		{"debian switch off", 1000, false, map[string]string{"/proc/sys/kernel/unprivileged_userns_clone": "0"}, false},
		{"ubuntu apparmor restriction", 1000, false, map[string]string{"/proc/sys/kernel/apparmor_restrict_unprivileged_userns": "1"}, false},
		{"userns allowed", 1000, false, map[string]string{"/proc/sys/user/max_user_namespaces": "63000", "/proc/sys/kernel/apparmor_restrict_unprivileged_userns": "0"}, true},
	}
	for _, c := range cases {
		if got := sandboxPossible(c.euid, c.flatpak, vals(c.proc)); got != c.want {
			t.Errorf("%s: sandboxPossible = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSandboxEnabled_EnvOverride(t *testing.T) {
	t.Setenv("VIBEZ_NO_SANDBOX", "1")
	if sandboxEnabled() {
		t.Error("VIBEZ_NO_SANDBOX=1 must disable the sandbox")
	}
}
