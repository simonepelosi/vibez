//go:build linux || darwin

package cdp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	playwright "github.com/mxschmitt/playwright-go"
)

const outdatedPlaywrightVersion = "1.57.0"

func playwrightPackageArchive(t *testing.T, version string) []byte {
	t.Helper()

	files := map[string]string{
		"package/package.json": fmt.Sprintf(`{"version":%q}`, version),
		"package/cli.js":       version,
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write Playwright package header: %v", err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("write Playwright package content: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close Playwright package tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close Playwright package gzip: %v", err)
	}
	return archive.Bytes()
}

func setTestCacheHome(t *testing.T) {
	t.Helper()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("HOME", cacheHome)
}

func setVersionReportingNode(t *testing.T) {
	t.Helper()
	fakeNodePath := filepath.Join(t.TempDir(), "node")
	fakeNode := `#!/bin/sh
printf 'Version '
cat "$1"
printf '\n'
`
	if err := os.WriteFile(fakeNodePath, []byte(fakeNode), 0o700); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake Node.js: %v", err)
	}
	t.Setenv("PLAYWRIGHT_NODEJS_PATH", fakeNodePath)
}

func TestInstallPlaywrightDriverReplacesOutdatedCache(t *testing.T) {
	setTestCacheHome(t)

	driver, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: driverDir()})
	if err != nil {
		t.Fatalf("create Playwright driver: %v", err)
	}
	if driver.Version == outdatedPlaywrightVersion {
		t.Fatalf("test requires an outdated version, current version is %q", driver.Version)
	}

	packageDir := filepath.Join(driverDir(), "package")
	if err := os.MkdirAll(packageDir, 0o750); err != nil {
		t.Fatalf("create stale driver cache: %v", err)
	}
	stalePackageJSON := fmt.Sprintf(`{"version":%q}`, outdatedPlaywrightVersion)
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(stalePackageJSON), 0o600); err != nil {
		t.Fatalf("write stale driver version: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "cli.js"), []byte(outdatedPlaywrightVersion), 0o600); err != nil {
		t.Fatalf("write stale driver CLI: %v", err)
	}
	markerPath := filepath.Join(driverDir(), "stale-marker")
	if err := os.WriteFile(markerPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale driver marker: %v", err)
	}

	setVersionReportingNode(t)

	archive := playwrightPackageArchive(t, driver.Version)
	registry := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		wantPath := fmt.Sprintf("/playwright-core/-/playwright-core-%s.tgz", driver.Version)
		if request.URL.Path != wantPath {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		_, _ = response.Write(archive)
	}))
	t.Cleanup(registry.Close)
	t.Setenv("PLAYWRIGHT_GO_NPM_REGISTRY", registry.URL)

	if err := installPlaywrightDriver(); err != nil {
		t.Fatalf("install Playwright driver over stale cache: %v", err)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("stale driver cache marker still exists: %v", err)
	}
	packageJSON, err := os.ReadFile(filepath.Join(packageDir, "package.json")) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read installed driver version: %v", err)
	}
	wantPackageJSON := fmt.Sprintf(`{"version":%q}`, driver.Version)
	if string(packageJSON) != wantPackageJSON {
		t.Fatalf("installed package.json = %s, want %s", packageJSON, wantPackageJSON)
	}
	versionOutput, err := driver.Command("--version").Output()
	if err != nil {
		t.Fatalf("run installed driver version probe: %v", err)
	}
	wantVersionOutput := fmt.Sprintf("Version %s\n", driver.Version)
	if string(versionOutput) != wantVersionOutput {
		t.Fatalf("installed driver version = %q, want %q", versionOutput, wantVersionOutput)
	}
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	}); err != nil {
		t.Fatalf("validate installed driver on a second install: %v", err)
	}
}

func TestPreparePlaywrightDriverInstallRemovesCacheWithStaleCLI(t *testing.T) {
	setTestCacheHome(t)

	driver, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: driverDir()})
	if err != nil {
		t.Fatalf("create Playwright driver: %v", err)
	}
	if driver.Version == outdatedPlaywrightVersion {
		t.Fatalf("test requires an outdated version, current version is %q", driver.Version)
	}
	packageDir := filepath.Join(driverDir(), "package")
	if err := os.MkdirAll(packageDir, 0o750); err != nil {
		t.Fatalf("create inconsistent driver cache: %v", err)
	}
	packageJSON := fmt.Sprintf(`{"version":%q}`, driver.Version)
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(packageJSON), 0o600); err != nil {
		t.Fatalf("write current driver metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "cli.js"), []byte(outdatedPlaywrightVersion), 0o600); err != nil {
		t.Fatalf("write stale driver CLI: %v", err)
	}
	setVersionReportingNode(t)

	if err := preparePlaywrightDriverInstall(); err != nil {
		t.Fatalf("prepare driver install: %v", err)
	}
	if _, err := os.Stat(driverDir()); !os.IsNotExist(err) {
		t.Fatalf("inconsistent driver cache still exists: %v", err)
	}
}

func TestPreparePlaywrightDriverInstallPreservesCurrentCache(t *testing.T) {
	setTestCacheHome(t)

	driver, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: driverDir()})
	if err != nil {
		t.Fatalf("create Playwright driver: %v", err)
	}
	packageDir := filepath.Join(driverDir(), "package")
	if err := os.MkdirAll(packageDir, 0o750); err != nil {
		t.Fatalf("create current driver cache: %v", err)
	}
	packageJSON := fmt.Sprintf(`{"version":%q}`, driver.Version)
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(packageJSON), 0o600); err != nil {
		t.Fatalf("write current driver version: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "cli.js"), []byte(driver.Version), 0o600); err != nil {
		t.Fatalf("write current driver CLI: %v", err)
	}
	setVersionReportingNode(t)
	markerPath := filepath.Join(driverDir(), "current-marker")
	if err := os.WriteFile(markerPath, []byte("current"), 0o600); err != nil {
		t.Fatalf("write current driver marker: %v", err)
	}

	if err := preparePlaywrightDriverInstall(); err != nil {
		t.Fatalf("prepare driver install: %v", err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("current driver cache was removed: %v", err)
	}
	versionOutput, err := driver.Command("--version").Output()
	if err != nil {
		t.Fatalf("run preserved driver version probe: %v", err)
	}
	wantVersionOutput := fmt.Sprintf("Version %s\n", driver.Version)
	if string(versionOutput) != wantVersionOutput {
		t.Fatalf("preserved driver version = %q, want %q", versionOutput, wantVersionOutput)
	}
}
