//go:build linux || darwin

package cdp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	playwright "github.com/mxschmitt/playwright-go"
)

func isDriverUpToDate() bool {
	driver, err := playwright.NewDriver(&playwright.RunOptions{
		DriverDirectory: driverDir(),
	})
	if err != nil {
		return false
	}
	packageJSONPath := filepath.Join(driverDir(), "package", "package.json")
	data, err := os.ReadFile(packageJSONPath) //nolint:gosec // path constructed from cache dir
	if err != nil {
		return false
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	return pkg.Version == driver.Version
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
