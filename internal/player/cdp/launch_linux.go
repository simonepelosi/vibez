//go:build linux

package cdp

import (
	playwright "github.com/mxschmitt/playwright-go"
)

// launchBrowser starts Chromium and returns the page to drive plus a close
// function.
//
// With a system browser (linux/arm64, or a VIBEZ_CHROME_PATH override) it uses
// a persistent profile (chromiumProfileDir) so the Widevine component "hint
// file", written during EnsureBrowser's warm-up, is read and the CDM loads. An
// ephemeral profile (the default Launch) never loads Widevine there because
// the hint only takes effect on a subsequent launch. The bundled amd64 Chrome
// carries Widevine directly, so an ephemeral launch is used.
//
// Playwright injects --mute-audio into every headless Chromium launch; we strip
// it so audio routes through PulseAudio/PipeWire. Playwright also injects
// --disable-component-update; we strip it so Chrome can load the Widevine CDM
// component. DRM in headless mode is enabled via --headless=new (chromeLaunchArgs).
func launchBrowser(pw *playwright.Playwright, chromePath string, headless, wsl bool) (playwright.Page, func(), error) {
	sandbox := sandboxEnabled()
	pg, closeFn, err := launchOnce(pw, chromePath, headless, wsl, sandbox)
	if err != nil && sandbox {
		// sandboxEnabled is a heuristic over /proc and the environment. When
		// Chromium still cannot create its sandbox, run without it rather
		// than leave the user with no playback.
		return launchOnce(pw, chromePath, headless, wsl, false)
	}
	return pg, closeFn, err
}

func launchOnce(pw *playwright.Playwright, chromePath string, headless, wsl, sandbox bool) (playwright.Page, func(), error) {
	ignore := []string{"--mute-audio", "--disable-component-update"}
	args := chromeLaunchArgsSandbox(headless, wsl, sandbox)

	if useSystemBrowser() {
		ctx, err := pw.Chromium.LaunchPersistentContext(chromiumProfileDir(), playwright.BrowserTypeLaunchPersistentContextOptions{
			ExecutablePath:    &chromePath,
			Headless:          &headless,
			ChromiumSandbox:   &sandbox,
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
		ChromiumSandbox:   &sandbox,
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
