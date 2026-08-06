//go:build linux || darwin

package cdp

import (
	"bytes"
	"fmt"
	"os"

	playwright "github.com/mxschmitt/playwright-go"
)

func isDriverUpToDate() bool {
	driver, err := playwright.NewDriver(&playwright.RunOptions{
		DriverDirectory: driverDir(),
	})
	if err != nil {
		return false
	}
	output, err := driver.Command("--version").Output()
	if err != nil {
		return false
	}
	return bytes.Contains(output, []byte(driver.Version))
}

func preparePlaywrightDriverInstall() error {
	if isDriverUpToDate() {
		return nil
	}
	if err := os.RemoveAll(driverDir()); err != nil {
		return fmt.Errorf("remove outdated Playwright driver cache: %w", err)
	}
	return nil
}

func installPlaywrightDriver() error {
	if err := preparePlaywrightDriverInstall(); err != nil {
		return err
	}
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	}); err != nil {
		return fmt.Errorf("playwright driver: %w", err)
	}
	return nil
}
