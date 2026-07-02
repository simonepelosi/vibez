//go:build darwin

package cdp

import (
	"fmt"
	"os"
	"path/filepath"

	playwright "github.com/playwright-community/playwright-go"
)

const chromeInstallHelp = "install Google Chrome from https://www.google.com/chrome/ or set VIBEZ_CHROME_PATH/CHROME_PATH"

func baseDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Caches", "vibez")
}

func driverDir() string { return filepath.Join(baseDir(), "driver") }

func ChromePath() string {
	path, _ := findChromePath()
	return path
}

func HelperPath() string { return ChromePath() }

func findChromePath() (string, error) {
	if path := os.Getenv("VIBEZ_CHROME_PATH"); path != "" {
		return validateChromePath("VIBEZ_CHROME_PATH", path)
	}
	if path := os.Getenv("CHROME_PATH"); path != "" {
		return validateChromePath("CHROME_PATH", path)
	}
	for _, path := range chromeCandidates() {
		if chrome, err := validateChromePath("", path); err == nil {
			return chrome, nil
		}
	}
	return "", fmt.Errorf("Google Chrome not found; %s", chromeInstallHelp)
}

func chromeCandidates() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		filepath.Join(home, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
	}
}

func validateChromePath(source, path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		if source != "" {
			return "", fmt.Errorf("%s=%q is not usable: %w", source, path, err)
		}
		return "", err
	}
	if st.IsDir() {
		if source != "" {
			return "", fmt.Errorf("%s=%q is a directory, not a Chrome executable", source, path)
		}
		return "", fmt.Errorf("%q is a directory", path)
	}
	if st.Mode()&0o111 == 0 {
		if source != "" {
			return "", fmt.Errorf("%s=%q is not executable", source, path)
		}
		return "", fmt.Errorf("%q is not executable", path)
	}
	return path, nil
}

// EnsureBrowser verifies an installed Google Chrome and installs only the
// Playwright driver. Chrome is not auto-downloaded on macOS.
func EnsureBrowser(onProgress func(string)) error {
	chromePath, err := findChromePath()
	if err != nil {
		return err
	}

	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	onProgress(fmt.Sprintf("Using Google Chrome: %s", chromePath))
	onProgress("Fetching browser driver...")
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	}); err != nil {
		return fmt.Errorf("playwright driver: %w", err)
	}
	return nil
}

func runPlaywright() (*playwright.Playwright, error) {
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	pw, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	})
	if err != nil {
		return nil, fmt.Errorf("playwright driver: %w", err)
	}
	return pw, nil
}

func chromeLaunchArgs(headless bool, _ bool) []string {
	args := []string{
		"--autoplay-policy=no-user-gesture-required",
		"--enable-features=MediaCapabilities,WidevineCdm",
		"--disable-blink-features=AutomationControlled",
		"--disable-background-networking",
		"--js-flags=--max-old-space-size=256",
	}
	// We do NOT use --headless=new on macOS because Widevine DRM is unsupported
	// in any headless mode on macOS due to VMP (Verified Media Path) constraints.
	return args
}
