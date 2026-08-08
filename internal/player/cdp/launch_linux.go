//go:build linux

package cdp

import (
	"runtime"

	playwright "github.com/mxschmitt/playwright-go"
)

// launchBrowser starts Chromium and returns the page to drive plus a close
// function.
//
// On linux/arm64 it uses a persistent profile (chromiumProfileDir) so the
// Widevine component "hint file" — written during EnsureBrowser's warm-up — is
// read and the CDM loads. An ephemeral profile (the default Launch) never loads
// Widevine there because the hint only takes effect on a subsequent launch. On
// amd64, Chrome bundles Widevine directly, so an ephemeral launch is used.
//
// Playwright injects --mute-audio into every headless Chromium launch; we strip
// it so audio routes through PulseAudio/PipeWire. Playwright also injects
// --disable-component-update; we strip it so Chrome can load the Widevine CDM
// component. DRM in headless mode is enabled via --headless=new (chromeLaunchArgs).
func launchBrowser(pw *playwright.Playwright, chromePath string, headless, wsl bool) (playwright.Page, func(), error) {
	ignore := []string{"--mute-audio", "--disable-component-update"}
	args := chromeLaunchArgs(headless, wsl)

	if runtime.GOARCH == "arm64" {
		ctx, err := pw.Chromium.LaunchPersistentContext(chromiumProfileDir(), playwright.BrowserTypeLaunchPersistentContextOptions{
			ExecutablePath:    &chromePath,
			Headless:          &headless,
			IgnoreDefaultArgs: ignore,
			Args:              args,
		})
		if err != nil {
			return nil, nil, err
		}
		pg, err := ctx.NewPage()
		if err != nil {
			_ = ctx.Close()
			return nil, nil, err
		}
		return pg, func() { _ = ctx.Close() }, nil
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		ExecutablePath:    &chromePath,
		Headless:          &headless,
		IgnoreDefaultArgs: ignore,
		Args:              args,
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
