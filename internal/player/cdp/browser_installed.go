//go:build darwin || windows

package cdp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	playwright "github.com/mxschmitt/playwright-go"
)

const chromeInstallHelp = "install Google Chrome from https://www.google.com/chrome/ or set VIBEZ_CHROME_PATH/CHROME_PATH"

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
	return "", fmt.Errorf("could not find Google Chrome; %s", chromeInstallHelp)
}

func validateChromePath(source, path string) (string, error) {
	st, err := os.Stat(path) //nolint:gosec // explicit browser override or local installation candidate
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
	if runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		if source != "" {
			return "", fmt.Errorf("%s=%q is not executable", source, path)
		}
		return "", fmt.Errorf("%q is not executable", path)
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return "", fmt.Errorf("%s=%q must name a Chrome .exe file", source, path)
	}
	return path, nil
}

// EnsureBrowser verifies an installed Google Chrome and installs only the
// Playwright driver. Chrome is not auto-downloaded on macOS or Windows.
func EnsureBrowser(onProgress func(string)) error {
	chromePath, err := findChromePath()
	if err != nil {
		return err
	}

	onProgress(fmt.Sprintf("Using Google Chrome: %s", chromePath))
	onProgress("Fetching browser driver...")
	if err := installPlaywrightDriver(); err != nil {
		return err
	}
	return nil
}

func runPlaywright() (*playwright.Playwright, error) {
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	opts, output := newDriverRunOptions(driverDir())
	pw, err := playwright.Run(opts)
	if err != nil {
		return nil, addDriverOutput(fmt.Errorf("playwright driver: %w", err), output)
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
	if headless {
		args = append(args, "--headless=new")
	}
	return args
}
