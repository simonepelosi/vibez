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
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	playwright "github.com/playwright-community/playwright-go"
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

// ChromePath returns the path to the cached Chrome binary.
func ChromePath() string {
	return filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "chrome")
}

// HelperPath returns the path to the vibez-helper hard link of the Chrome
// binary. Launching Chrome via this path causes the process (and child
// processes that re-exec via /proc/self/exe) to appear as "vibez-helper"
// in ps/top instead of "chrome".
func HelperPath() string {
	return filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "vibez-helper")
}

// linkHelper creates a hard link vibez-helper → chrome so the spawned
// process shows as "vibez-helper" in process listings. Idempotent.
func linkHelper() {
	if _, err := os.Stat(HelperPath()); err == nil {
		return // already exists
	}
	_ = os.Link(ChromePath(), HelperPath())
}

// EnsureBrowser downloads and extracts Google Chrome into vibez's private
// cache directory if not already present. Never calls apt-get or sudo.
// onProgress is called with human-readable status strings (e.g. "Downloading
// Chrome… 42%", "Extracting Chrome…"). Pass func(string){} to silence.
func EnsureBrowser(onProgress func(string)) error {
	if _, err := os.Stat(ChromePath()); err == nil {
		linkHelper() // idempotent — creates vibez-helper link if absent
		return nil   // already installed
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

func chromeLaunchArgs(wsl bool) []string {
	widevinePath := filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "WidevineCdm")
	return launchArgs(widevinePath, wsl)
}

func launchArgs(widevinePath string, wsl bool) []string {
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
		"--widevine-path=" + widevinePath,
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

	if wsl {
		// WSL2/PulseAudio: increase audio buffering to absorb Hyper-V scheduler jitter.
		args = append(args, "--audio-buffer-size=4096")
	}

	return args
}
