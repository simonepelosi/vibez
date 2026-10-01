//go:build linux || darwin || windows

package cdp

import (
	"fmt"
	"os"
	"sync"

	playwright "github.com/mxschmitt/playwright-go"
)

// OpenBrowser starts a private Chrome session. Call EnsureBrowser first.
// The returned close function releases both Chrome and the Playwright driver.
func OpenBrowser(headless, wsl bool) (playwright.Page, func(), error) {
	pw, err := runPlaywright()
	if err != nil {
		return nil, nil, err
	}
	chromePath := HelperPath()
	if _, err := os.Stat(chromePath); err != nil {
		chromePath = ChromePath()
	}
	page, closeBrowser, err := launchBrowser(pw, chromePath, headless, wsl)
	if err != nil {
		_ = pw.Stop()
		return nil, nil, fmt.Errorf("launch Chrome: %w", err)
	}
	var once sync.Once
	return page, func() {
		once.Do(func() {
			closeBrowser()
			_ = pw.Stop()
		})
	}, nil
}
