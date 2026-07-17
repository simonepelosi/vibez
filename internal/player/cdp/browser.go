//go:build linux

// Package cdp provides an Apple Music player backed by a private Chrome
// installation managed entirely by vibez. On first run, vibez downloads the
// Google Chrome .deb (~130 MB) from Google's public CDN, extracts it without
// requiring dpkg-deb or root (using a pure-Go ar parser + system tar), and
// caches the result in ~/.cache/vibez/chrome/.
// Subsequent launches use the cached binary instantly — no system packages,
// no apt-get, no sudo, invisible to the rest of the OS.
// Widevine CDM is bundled inside Chrome and is available automatically.
package cdp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

const chromeDebURL = "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"

func baseDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez")
}

// chromeInstallDir is where the Chrome .deb is extracted.
func chromeInstallDir() string { return filepath.Join(baseDir(), "chrome") }

// driverDir is where the Playwright Node.js driver lives.
func driverDir() string { return filepath.Join(baseDir(), "driver") }

// ChromePath returns the path to the Chrome/Chromium binary vibez launches.
// On amd64 this is the privately downloaded Google Chrome; on arm64 — where
// Google publishes no Linux build — it is a discovered system Chromium/Chrome.
func ChromePath() string {
	if runtime.GOARCH == "arm64" {
		path, _ := findSystemBrowser()
		return path
	}
	return bundledChromePath()
}

// bundledChromePath is the amd64 layout: the extracted Google Chrome .deb.
func bundledChromePath() string {
	return filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "chrome")
}

// HelperPath returns the path to the vibez-helper hard link of the Chrome
// binary. Launching Chrome via this path causes the process (and child
// processes that re-exec via /proc/self/exe) to appear as "vibez-helper"
// in ps/top instead of "chrome". On arm64 the browser lives in a read-only
// system location we cannot hard-link into, so it equals ChromePath().
func HelperPath() string {
	if runtime.GOARCH == "arm64" {
		return ChromePath()
	}
	return filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "vibez-helper")
}

// linkHelper creates a hard link vibez-helper → chrome so the spawned
// process shows as "vibez-helper" in process listings. Idempotent. No-op on
// arm64, where the system browser is not owned by vibez.
func linkHelper() {
	if runtime.GOARCH == "arm64" {
		return
	}
	if _, err := os.Stat(HelperPath()); err == nil {
		return // already exists
	}
	_ = os.Link(ChromePath(), HelperPath())
}

// chromeInstallHelpARM64 guides arm64 users when a browser or Widevine CDM is
// missing. Google ships no Linux/arm64 Chrome, so vibez relies on a
// system-installed Chromium plus a system-registered Widevine CDM.
const chromeInstallHelpARM64 = "install Chromium and a Widevine CDM (e.g. `pacman -S chromium widevine` on Arch Linux ARM, or your distro's equivalent) — or set VIBEZ_CHROME_PATH to a browser binary"

// systemBrowserCandidates lists the executables searched on PATH (in order)
// for the arm64 full-track backend.
func systemBrowserCandidates() []string {
	return []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome"}
}

// systemBrowserRealBinaries lists absolute paths to real browser binaries,
// preferred over PATH entries. On several distros the PATH `chromium` is a
// launcher wrapper that injects flags from config files; launching the real
// binary avoids that and matches how vibez launches Chrome on amd64/macOS.
func systemBrowserRealBinaries() []string {
	return []string{
		"/usr/lib/chromium/chromium",
		"/usr/lib64/chromium/chromium",
		"/usr/lib/chromium-browser/chromium-browser",
		"/opt/google/chrome/chrome",
		"/opt/chromium.org/chromium/chromium",
	}
}

func usableExecutable(p string) bool {
	st, err := os.Stat(p) //nolint:gosec // G703: path is an operator-provided (VIBEZ_CHROME_PATH) or fixed system browser path, not attacker input
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// findSystemBrowser locates a Chromium/Chrome executable for the arm64 path.
// Order: VIBEZ_CHROME_PATH / CHROME_PATH overrides, then known real binaries,
// then whatever launcher is on PATH.
func findSystemBrowser() (string, error) {
	for _, env := range []string{"VIBEZ_CHROME_PATH", "CHROME_PATH"} {
		if p := os.Getenv(env); p != "" {
			if !usableExecutable(p) {
				return "", fmt.Errorf("%s=%q is not a usable browser executable", env, p)
			}
			return p, nil
		}
	}
	for _, p := range systemBrowserRealBinaries() {
		if usableExecutable(p) {
			return p, nil
		}
	}
	for _, name := range systemBrowserCandidates() {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Chromium/Chrome found on PATH; %s", chromeInstallHelpARM64)
}

// widevineCDMDir returns the first directory holding an arm64 Widevine CDM, or
// "" if none is found. It checks well-known package locations plus the
// directory next to the discovered browser (where Chromium keeps its CDM).
// Chromium can also locate a system-registered CDM on its own, so callers may
// launch without an explicit --widevine-path even when this returns "".
// widevineSystemDirs are the fixed locations checked for a registered Widevine
// CDM. Declared as a var so tests can substitute a controlled list.
var widevineSystemDirs = []string{
	"/usr/lib/chromium/WidevineCdm",
	"/usr/lib64/chromium/WidevineCdm",
	"/usr/lib/chromium-browser/WidevineCdm",
	"/opt/google/chrome/WidevineCdm",
	"/opt/WidevineCdm/chromium",
	"/var/lib/widevine/WidevineCdm",
}

func widevineCDMDir() string {
	candidates := append([]string(nil), widevineSystemDirs...)
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "lib", "widevine", "WidevineCdm"))
	}
	// The CDM commonly sits next to the browser binary (e.g. Arch's
	// /usr/lib/chromium/WidevineCdm alongside /usr/lib/chromium/chromium).
	if b, err := findSystemBrowser(); err == nil {
		if real, err := filepath.EvalSymlinks(b); err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(real), "WidevineCdm"))
		}
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "_platform_specific", "linux_arm64", "libwidevinecdm.so")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "libwidevinecdm.so")); err == nil {
			return dir
		}
	}
	return ""
}

// Available reports whether the full-track Chrome/CDP backend can run on this
// host. On amd64 vibez downloads Google Chrome, so it is always available. On
// arm64 it requires a system Chromium/Chrome plus a Widevine CDM. Every other
// Linux arch uses the WebKit + GStreamer preview fallback.
func Available() bool {
	switch runtime.GOARCH {
	case "amd64":
		return true
	case "arm64":
		if _, err := findSystemBrowser(); err != nil {
			return false
		}
		return widevineCDMDir() != ""
	default:
		return false
	}
}

// EnsureBrowser downloads and extracts Google Chrome into vibez's private
// cache directory if not already present. Never calls apt-get or sudo.
// onProgress is called with human-readable status strings (e.g. "Downloading
// Chrome… 42%", "Extracting Chrome…"). Pass func(string){} to silence.
func isDriverUpToDate() bool {
	driver, err := playwright.NewDriver(&playwright.RunOptions{
		DriverDirectory: driverDir(),
	})
	if err != nil {
		return false
	}
	pkgJSONPath := filepath.Join(driverDir(), "package", "package.json")
	data, err := os.ReadFile(pkgJSONPath) //nolint:gosec // path constructed from cache dir
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

func EnsureBrowser(onProgress func(string)) error {
	if runtime.GOARCH == "arm64" {
		return ensureBrowserARM64(onProgress)
	}
	return ensureBrowserAMD64(onProgress)
}

// ensureBrowserARM64 prepares the arm64 full-track backend. Google publishes no
// Linux/arm64 Chrome, so rather than download a browser it verifies a system
// Chromium/Chrome and a system-registered Widevine CDM are present, then
// fetches only the arch-aware Playwright Node driver.
func ensureBrowserARM64(onProgress func(string)) error {
	browser, err := findSystemBrowser()
	if err != nil {
		return err
	}
	onProgress(fmt.Sprintf("Using system browser: %s", browser))
	if widevineCDMDir() == "" {
		return fmt.Errorf("no Widevine CDM found (required for full-track playback); %s", chromeInstallHelpARM64)
	}

	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	onProgress("Fetching dependencies…")
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	}); err != nil {
		return fmt.Errorf("playwright driver: %w", err)
	}
	if err := warmUpWidevineARM64(onProgress); err != nil {
		return err
	}
	onProgress("Browser ready.")
	return nil
}

// chromiumProfileDir is the persistent Chromium profile vibez uses on arm64.
// A persistent profile is required because the system Chromium registers its
// Widevine CDM through the component-updater, which writes a "hint file" into
// the profile on first launch that only takes effect on subsequent launches.
// An ephemeral profile (Playwright's default) would never load Widevine.
func chromiumProfileDir() string { return filepath.Join(baseDir(), "chromium-arm64") }

// widevineHintFile is the marker Chromium writes once it has registered the
// preinstalled Widevine CDM into chromiumProfileDir.
func widevineHintFile() string {
	return filepath.Join(chromiumProfileDir(), "WidevineCdm", "latest-component-updated-widevine-cdm")
}

// warmUpWidevineARM64 launches the system Chromium once against the persistent
// profile so its component-updater registers the preinstalled Widevine CDM and
// writes the hint file. It is a no-op once the hint file exists. Without this,
// the first playback session would silently fall back to previews.
func warmUpWidevineARM64(onProgress func(string)) error {
	if _, err := os.Stat(widevineHintFile()); err == nil {
		return nil // Widevine already registered in this profile
	}
	browser, err := findSystemBrowser()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(chromiumProfileDir(), 0o750); err != nil {
		return fmt.Errorf("create chromium profile dir: %w", err)
	}
	onProgress("Registering Widevine CDM…")
	cmd := exec.Command(browser, //nolint:gosec // path from trusted discovery
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--user-data-dir="+chromiumProfileDir(), "about:blank")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("warm-up launch: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(widevineHintFile()); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for the Widevine CDM to register; %s", chromeInstallHelpARM64)
}

// ensureBrowserAMD64 downloads and extracts Google Chrome into vibez's private
// cache directory if not already present. Never calls apt-get or sudo.
func ensureBrowserAMD64(onProgress func(string)) error {
	driverUpToDate := isDriverUpToDate()
	chromeInstalled := false
	if _, err := os.Stat(ChromePath()); err == nil {
		chromeInstalled = true
	}

	if driverUpToDate && chromeInstalled {
		linkHelper()
		return nil
	}

	// Also ensure the playwright driver is available (no browser install via playwright).
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	onProgress("Fetching dependencies…")
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	}); err != nil {
		return fmt.Errorf("playwright driver: %w", err)
	}

	if chromeInstalled {
		linkHelper()
		return nil
	}

	debPath := filepath.Join(baseDir(), "chrome.deb")
	if err := downloadFile(onProgress, debPath, chromeDebURL); err != nil {
		return fmt.Errorf("download Chrome: %w", err)
	}
	defer os.Remove(debPath) //nolint:errcheck // best-effort cleanup of temp file

	onProgress("Extracting Chromium drivers…")
	if err := extractDeb(debPath, chromeInstallDir()); err != nil {
		return fmt.Errorf("extract chrome: %w", err)
	}

	if _, err := os.Stat(ChromePath()); err != nil {
		return fmt.Errorf("chrome binary not found after extraction: %w", err)
	}
	linkHelper()
	onProgress("Chrome ready.")
	return nil
}

// extractDeb unpacks the payload of a Debian .deb archive into destDir without
// requiring dpkg-deb (which is absent in sandboxed environments like Flatpak).
//
// A .deb is an ar(1) archive containing three members:
//
//	debian-binary   — format version ("2.0\n")
//	control.tar.*   — package metadata (ignored here)
//	data.tar.*      — the actual filesystem payload (xz / gz / zst / bz2)
//
// We parse the ar header in pure Go to locate data.tar.*, write it to a
// temporary file, then delegate decompression+extraction to system tar, which
// auto-detects the compression format and is guaranteed to be present on any
// Linux system (including the GNOME Platform Flatpak runtime).
func extractDeb(debPath, destDir string) error {
	f, err := os.Open(debPath) //nolint:gosec // path constructed from cache dir
	if err != nil {
		return fmt.Errorf("open deb: %w", err)
	}
	defer f.Close() //nolint:errcheck

	// Verify the ar magic header.
	magic := make([]byte, 8)
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != "!<arch>\n" {
		return fmt.Errorf("not a valid ar archive")
	}

	// Walk ar entries (each header is exactly 60 bytes).
	hdr := make([]byte, 60)
	for {
		if _, err := io.ReadFull(f, hdr); err != nil {
			break // EOF — data.tar.* was not found
		}
		name := strings.TrimRight(string(hdr[:16]), " ")
		size, _ := strconv.ParseInt(strings.TrimRight(string(hdr[48:58]), " "), 10, 64)

		if strings.HasPrefix(name, "data.tar") {
			// Write the embedded data.tar.* into a sibling temp file so that
			// tar can seek it (some tar builds require a seekable input).
			tmp, err := os.CreateTemp(filepath.Dir(debPath), "vibez-data.tar.*")
			if err != nil {
				return fmt.Errorf("create temp: %w", err)
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath) //nolint:errcheck,gocritic // deferInLoop: function always returns before next iteration

			if _, err := io.CopyN(tmp, f, size); err != nil {
				_ = tmp.Close()
				return fmt.Errorf("write data tar: %w", err)
			}
			_ = tmp.Close()

			if err := os.MkdirAll(destDir, 0o750); err != nil {
				return fmt.Errorf("create dest dir: %w", err)
			}
			// tar auto-detects xz / gz / zst / bz2 via the file's magic bytes.
			cmd := exec.Command("tar", "-xf", tmpPath, "-C", destDir) //nolint:gosec
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("tar extract: %w\n%s", err, out)
			}
			return nil
		}

		// Skip this entry's data (ar pads entries to even offsets).
		skip := size
		if skip%2 != 0 {
			skip++
		}
		if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
			return fmt.Errorf("seek past entry: %w", err)
		}
	}
	return fmt.Errorf("data.tar.* member not found in %s", filepath.Base(debPath))
}

// runPlaywright starts the Playwright driver backed by our cached Chrome.
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

func chromeLaunchArgs(headless bool, wsl bool) []string {
	var widevinePath string
	if runtime.GOARCH == "arm64" {
		// System Chromium locates its registered CDM itself; pass the path
		// only when we found one, so a non-default location still works.
		widevinePath = widevineCDMDir()
	} else {
		widevinePath = filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "WidevineCdm")
	}
	return launchArgs(widevinePath, headless, wsl)
}

func launchArgs(widevinePath string, headless bool, wsl bool) []string {
	disableFeatures := "HardwareMediaKeyHandling,MediaSessionService,CertificateTransparencyComponentUpdater"
	if wsl {
		// WSL2: disable out-of-process audio service to avoid distortion when
		// PulseAudio and Windows run at different sample rates (44100 vs 48000).
		disableFeatures += ",AudioServiceOutOfProcess"
	}

	args := []string{
		// Sandbox requires suid/namespace support unavailable from a non-system path.
		"--no-sandbox",
		"--disable-setuid-sandbox",
		// --no-zygote removes the Linux process-spawning shim; safe when sandbox
		// is already disabled and cuts one helper process.
		"--no-zygote",
		"--autoplay-policy=no-user-gesture-required",
		"--enable-features=MediaCapabilities,WidevineCdm",
		"--disable-blink-features=AutomationControlled",
		// Suppress Chrome's built-in MPRIS D-Bus registration so our Go
		// MPRIS server (org.mpris.MediaPlayer2.vibez) is the sole player
		// visible to the desktop environment.
		"--disable-features=" + disableFeatures,
		"--disable-component-update",
		// Memory footprint reduction:
		// Removes the GPU compositor process (~100-200 MB) - not needed for
		// audio-only headless playback; Widevine CDM runs in a utility process
		// and does not require GPU acceleration for audio DRM.
		"--disable-gpu",
		// Use /tmp for shared-memory segments instead of /dev/shm to avoid
		// exhausting the (often small) tmpfs mounted there.
		"--disable-dev-shm-usage",
		// Cap the V8 JavaScript heap at 256 MB. MusicKit.js runs comfortably
		// within this limit; without it Chrome can balloon to 500 MB+.
		"--js-flags=--max-old-space-size=256",
		// Disable background network activity (prefetch, DNS pre-resolve,
		// speculative connections). Not needed for a single-page music player.
		"--disable-background-networking",
	}

	// On amd64 this is always the bundled Chrome's CDM; on arm64 it is set only
	// when a CDM directory was discovered (otherwise Chromium self-locates it).
	if widevinePath != "" {
		args = append(args, "--widevine-path="+widevinePath)
	}

	if headless {
		args = append(args, "--headless=new")
	}

	if wsl {
		// WSL2/PulseAudio: increase audio buffering to absorb Hyper-V scheduler jitter.
		args = append(args, "--audio-buffer-size=4096")
	}

	return args
}
