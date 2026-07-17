//go:build darwin

package cdp

import playwright "github.com/mxschmitt/playwright-go"

// launchBrowser starts Chromium with an ephemeral profile and returns the page
// to drive plus a close function. macOS uses the installed Google Chrome, which
// bundles Widevine directly, so no persistent profile or warm-up is needed.
//
// Playwright injects --mute-audio into every headless Chromium launch; we strip
// it so audio routes normally. Playwright also injects --disable-component-update;
// we strip it so Chrome can load the Widevine CDM component.
func launchBrowser(pw *playwright.Playwright, chromePath string, headless, wsl bool) (playwright.Page, func(), error) {
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		ExecutablePath:    &chromePath,
		Headless:          &headless,
		IgnoreDefaultArgs: []string{"--mute-audio", "--disable-component-update"},
		Args:              chromeLaunchArgs(headless, wsl),
	})
	if err != nil {
		return nil, nil, err
	}
	pg, err := browser.NewPage()
	if err != nil {
		_ = browser.Close()
		return nil, nil, err
	}
	return pg, func() { _ = browser.Close() }, nil
}
