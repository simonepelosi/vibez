//go:build linux

// Package cdp provides an Apple Music player backed by a private Chrome
// installation managed entirely by vibez. On first run, vibez downloads the
// Google Chrome .deb (~130 MB) from Google's public CDN, extracts it without
// requiring dpkg-deb or root (using a pure-Go ar parser + system tar), and
// caches the result in ~/.cache/vibez/chrome/.
// Subsequent launches use the cached binary instantly — no system packages,
// no apt-get, no sudo, invisible to the rest of the OS.
// Widevine CDM is bundled inside Chrome and is available automatically.
//
// Setting VIBEZ_CHROME_PATH (or CHROME_PATH) to a browser binary makes vibez
// launch that browser instead and download nothing; see useSystemBrowser.
package cdp

import (
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

// browserOverride returns the environment variable and browser path a user
// set to bypass discovery, VIBEZ_CHROME_PATH taking precedence over
// CHROME_PATH, or two empty strings when neither is set. The path is not
// validated here; findSystemBrowser does that.
func browserOverride() (string, string) {
	for _, name := range []string{"VIBEZ_CHROME_PATH", "CHROME_PATH"} {
		if p := os.Getenv(name); p != "" {
			return name, p
		}
	}
	return "", ""
}

// useSystemBrowser reports whether vibez launches a browser it does not own:
// always on arm64, where Google publishes no Linux Chrome, and on any arch
// when browserOverride names one. In that mode nothing is downloaded, the
// vibez-helper hard link is skipped, and Widevine is looked up on the system
// instead of inside the private Chrome.
func useSystemBrowser() bool {
	if runtime.GOARCH == "arm64" {
		return true
	}
	_, p := browserOverride()
	return p != ""
}

// ChromePath returns the path to the Chrome/Chromium binary vibez launches:
// the privately downloaded Google Chrome, or a system Chromium/Chrome when
// useSystemBrowser says so.
func ChromePath() string {
	if useSystemBrowser() {
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
// in ps/top instead of "chrome". A system browser lives somewhere vibez cannot
// hard-link into, so there it equals ChromePath().
func HelperPath() string {
	if useSystemBrowser() {
		return ChromePath()
	}
	return filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "vibez-helper")
}

// linkHelper creates a hard link vibez-helper → chrome so the spawned
// process shows as "vibez-helper" in process listings. Idempotent. No-op for
// a system browser, which vibez does not own.
func linkHelper() {
	if useSystemBrowser() {
		return
	}
	if _, err := os.Stat(HelperPath()); err == nil {
		return // already exists
	}
	_ = os.Link(ChromePath(), HelperPath())
}

// systemBrowserHelp guides a user past a missing browser or Widevine CDM on
// the system-browser path. Which advice applies turns on how vibez got here.
// An override the user set is theirs to correct or to drop, and telling them
// to set the variable they already set, or to install a distro Chromium they
// never asked vibez to use, helps with neither. Dropping the override on amd64
// returns them to the Google Chrome vibez downloads, which carries its own
// CDM; arm64 has no such fallback, so there the advice stays "install one".
func systemBrowserHelp() string {
	env, _ := browserOverride()
	switch {
	case env == "":
		return "install Chromium and a Widevine CDM (e.g. `pacman -S chromium widevine` on Arch Linux ARM, or your distro's equivalent), or set VIBEZ_CHROME_PATH to a browser binary"
	case runtime.GOARCH == "amd64":
		return "point " + env + " at a browser with a Widevine CDM, or unset it to use the Google Chrome vibez downloads, which bundles one"
	default:
		return "point " + env + " at a browser with a Widevine CDM, or unset it to let vibez discover a system Chromium (e.g. `pacman -S chromium widevine` on Arch Linux ARM, or your distro's equivalent)"
	}
}

// systemBrowserCandidates lists the executables searched on PATH (in order)
// for the system-browser backend.
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

// findSystemBrowser locates a Chromium/Chrome executable for the
// system-browser path. Order: the VIBEZ_CHROME_PATH / CHROME_PATH override,
// then known real binaries, then whatever launcher is on PATH.
func findSystemBrowser() (string, error) {
	if env, p := browserOverride(); p != "" {
		if !usableExecutable(p) {
			return "", fmt.Errorf("%s=%q is not a usable browser executable", env, p)
		}
		return p, nil
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
	return "", fmt.Errorf("no Chromium/Chrome found on PATH; %s", systemBrowserHelp())
}

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

// widevinePlatformDir is the per-arch subdirectory a WidevineCdm bundle keeps
// its shared object in. Chromium's name for amd64 is x64.
func widevinePlatformDir() string {
	if runtime.GOARCH == "amd64" {
		return "linux_x64"
	}
	return "linux_" + runtime.GOARCH
}

// widevineCDMDir returns the first directory holding a Widevine CDM for this
// arch, or "" if none is found. It checks well-known package locations plus the
// directory next to the discovered browser (where Chromium keeps its CDM).
// "" is not on its own fatal at launch: chromeLaunchArgs omits --widevine-path
// for it and lets Chromium locate a registered CDM itself. It is fatal
// earlier, in ensureSystemBrowser, which refuses to start a system browser it
// could not find a CDM for rather than let playback degrade to previews
// without saying so.
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
		if _, err := os.Stat(filepath.Join(dir, "_platform_specific", widevinePlatformDir(), "libwidevinecdm.so")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "libwidevinecdm.so")); err == nil {
			return dir
		}
	}
	return ""
}

// Available reports whether the full-track Chrome/CDP backend can run on this
// host. On amd64 vibez downloads Google Chrome, so it is always available; a
// VIBEZ_CHROME_PATH override is not checked here, so an unusable one fails in
// EnsureBrowser, where the user sees why. On arm64 it requires a system
// Chromium/Chrome plus a Widevine CDM. Every other Linux arch uses the WebKit
// + GStreamer preview fallback.
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
func EnsureBrowser(onProgress func(string)) error {
	if useSystemBrowser() {
		return ensureSystemBrowser(onProgress)
	}
	return ensureBrowserAMD64(onProgress)
}

// ensureSystemBrowser prepares the full-track backend around a browser vibez
// does not own: always on arm64, where Google publishes no Linux Chrome, and
// wherever VIBEZ_CHROME_PATH / CHROME_PATH points at one. Rather than download
// a browser it verifies a Chromium/Chrome and a Widevine CDM are present, then
// fetches only the arch-aware Playwright Node driver.
func ensureSystemBrowser(onProgress func(string)) error {
	browser, err := findSystemBrowser()
	if err != nil {
		return err
	}
	onProgress(fmt.Sprintf("Using system browser: %s", browser))
	if widevineCDMDir() == "" {
		return fmt.Errorf("no Widevine CDM found (required for full-track playback); %s", systemBrowserHelp())
	}

	onProgress("Fetching dependencies…")
	if err := installPlaywrightDriver(); err != nil {
		return err
	}
	if err := warmUpWidevine(onProgress); err != nil {
		return err
	}
	onProgress("Browser ready.")
	return nil
}

// chromiumProfileDir is the persistent Chromium profile vibez uses with a
// system browser: chromium-arm64 on arm64, as before, chromium-amd64 for an
// override there. A persistent profile is required because the system
// Chromium registers its Widevine CDM through the component-updater, which
// writes a "hint file" into the profile on first launch that only takes effect
// on subsequent launches. An ephemeral profile (Playwright's default) would
// never load Widevine.
func chromiumProfileDir() string { return filepath.Join(baseDir(), "chromium-"+runtime.GOARCH) }

// widevineHintFile is the marker Chromium writes once it has registered the
// preinstalled Widevine CDM into chromiumProfileDir.
func widevineHintFile() string {
	return filepath.Join(chromiumProfileDir(), "WidevineCdm", "latest-component-updated-widevine-cdm")
}

// warmUpWidevine launches the system Chromium once against the persistent
// profile so its component-updater registers the preinstalled Widevine CDM and
// writes the hint file. It is a no-op once the hint file exists. Without this,
// the first playback session would silently fall back to previews.
func warmUpWidevine(onProgress func(string)) error {
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
	args := []string{"--headless=new", "--disable-gpu", "--disable-dev-shm-usage"}
	if !sandboxEnabled() {
		args = append(args, "--no-sandbox")
	}
	args = append(args, "--user-data-dir="+chromiumProfileDir(), "about:blank")
	cmd := exec.Command(browser, args...) //nolint:gosec // path from trusted discovery
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
	return fmt.Errorf("timed out waiting for the Widevine CDM to register; %s", systemBrowserHelp())
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
	onProgress("Fetching dependencies…")
	if err := installPlaywrightDriver(); err != nil {
		return err
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
	opts, output := newDriverRunOptions(driverDir())
	pw, err := playwright.Run(opts)
	if err != nil {
		return nil, addDriverOutput(fmt.Errorf("playwright driver: %w", err), output)
	}
	return pw, nil
}

// chromeLaunchArgs returns the Chromium arguments, with the sandbox on unless
// sandboxEnabled says it cannot be.
func chromeLaunchArgs(headless bool, wsl bool) []string {
	return chromeLaunchArgsSandbox(headless, wsl, sandboxEnabled())
}

// chromeLaunchArgsSandbox is chromeLaunchArgs with the sandbox choice made by
// the caller, so a launch that fails sandboxed can be retried without it.
func chromeLaunchArgsSandbox(headless bool, wsl bool, sandbox bool) []string {
	var widevinePath string
	if useSystemBrowser() {
		// A system browser locates its registered CDM itself; pass the path
		// only when we found one, so a non-default location still works.
		widevinePath = widevineCDMDir()
	} else {
		widevinePath = filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "WidevineCdm")
	}
	return launchArgs(widevinePath, headless, wsl, sandbox)
}

func launchArgs(widevinePath string, headless bool, wsl bool, sandbox bool) []string {
	disableFeatures := "HardwareMediaKeyHandling,MediaSessionService,CertificateTransparencyComponentUpdater"
	if wsl {
		// WSL2: disable out-of-process audio service to avoid distortion when
		// PulseAudio and Windows run at different sample rates (44100 vs 48000).
		disableFeatures += ",AudioServiceOutOfProcess"
	}

	var args []string
	if !sandbox {
		// Only where Chromium cannot create a sandbox (see sandboxEnabled).
		// --no-zygote removes the Linux process-spawning shim and is only
		// valid without the sandbox, which needs the zygote to set itself up.
		args = append(args, "--no-sandbox", "--disable-setuid-sandbox", "--no-zygote")
	}
	args = append(args,
		"--autoplay-policy=no-user-gesture-required",
		"--enable-features=MediaCapabilities,WidevineCdm",
		"--disable-blink-features=AutomationControlled",
		// Suppress Chrome's built-in MPRIS D-Bus registration so our Go
		// MPRIS server (org.mpris.MediaPlayer2.vibez) is the sole player
		// visible to the desktop environment.
		"--disable-features="+disableFeatures,
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
	)

	// With the bundled Chrome this is its own CDM; with a system browser it is
	// set only when a CDM directory was discovered (otherwise Chromium
	// self-locates it).
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
