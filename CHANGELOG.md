# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- Automatic session re-authentication: when the Apple Music token expires vibez
  opens the browser silently and injects the fresh token into the running player —
  no manual `vibez auth login` step required.
- Persistent TUI banner when a session is expired
  (`"Session expired — opening browser to re-authenticate…"`).
- MIT `LICENSE` file.
- CI test pipeline (`ci.yml`) — runs `go build`, `go vet`, `go test` on every
  push and pull request to `main`.
- `.desktop` file and 512 × 512 icon bundled into the Flatpak so vibez appears
  in application launchers with `Terminal=true`.

### Changed
- README redesigned: centered logo, shields.io badges (CI, release, Go version,
  license), concise feature list, cleaner section layout.
- CI badge now tracks the `ci.yml` test workflow instead of `release.yml`.
- GoReleaser version pinned to `~> v2` (silences deprecation warning).
- **Search quality**: catalog search now goes through `amp-api.music.apple.com`
  (same endpoint as the Apple Music web player and Cider), which returns
  `extendedAssetUrls` in results. Songs without streaming URLs — purchase-only
  or region-locked tracks — are filtered out before they can appear in the list.
- **Search debounce**: the API call now fires only after 400 ms of typing
  inactivity. Intermediate keystrokes are discarded via a generation counter,
  so rapid typing no longer triggers multiple parallel searches.

---

## [0.0.1] — 2025-04-02

First public pre-release of vibez.

### Added

#### Playback
- Full Apple Music track streaming via headless Chrome + Widevine DRM (no
  external player needed).
- WebKit + GStreamer fallback when Chrome is unavailable (30-second preview
  URLs from the Apple Music catalog).
- Auto-detection of the best available audio engine at startup.
- MPRIS D-Bus server — desktop media keys, notifications and system players
  (e.g. KDE Connect) work out of the box.
- Repeat (off / all / one) and shuffle modes.
- Volume control (`+` / `-`).
- Chrome single-process mode enabled by default to minimise memory usage.

#### TUI
- Full-screen Bubble Tea TUI with alt-screen support.
- Animated bear mascot: sleeps when idle, dances when music plays.
- Pulsing braille spinner during loading and buffering.
- Real-time search panel (`/`) — searches the Apple Music catalog; results
  stream in as you type.
- Queue panel (`q`) — shows upcoming tracks; add with `tab`, skip with `n`/`p`.
- Library panel (`l`) — browse personal playlists, albums and tracks with tab
  navigation.
- Now Playing bar: track, artist, album, progress bar, position / duration.
- Two-line status bar with mode indicator and command palette.
- Command palette (`:`) — `:save <name>` saves the current queue as a playlist.
- Vibe panel with mood/energy display.
- Discovery: automatically queues a related song ~30 s before the current
  track ends.
- Favorites (`f`) — love / unlove the current track.
- Color-tuned palette with lipgloss styles.
- Loading screen with live progress (Chrome download %, auth status, engine
  init).
- Demo mode (`--demo`) for UI development without an Apple account.

#### Auth & Configuration
- `vibez auth login` — opens the system browser for Apple Music OAuth; token
  saved to `~/.config/vibez/config.json`.
- `vibez auth status` / `vibez auth logout` subcommands.
- Auto-detection of the Apple Music storefront from the user account.
- Apple Developer Token embedded at build time (obfuscated with garble in
  release builds).
- `scripts/gen-devtoken` — helper to generate a signed MusicKit JWT from a
  `.p8` private key.

#### Distribution
- Flatpak bundle (amd64) published to GitHub Releases.
- GoReleaser pipeline: garble-obfuscated binary, `tar.gz` + checksums.
- GitHub Actions release workflow: build → Flatpak prep → Flatpak bundle.

### Fixed
- Unplayable and region-restricted catalog tracks silently skipped (multi-layer
  `CONTENT_RESTRICTED` / `NOT_FOUND` / `CONTENT_EQUIVALENT` handling).
- Spurious auto-advance on manual `next` / `previous`.
- Queue panel overlay and key conflicts resolved.
- Playlist save reliability improvements.
- `ERR_CERT_VERIFIER_CHANGED` breaking MusicKit.js CDN load in Chrome.
- Bear mascot returning to idle state on `n` (next) keypress.
- MPRIS already triggers desktop-environment notifications — removed duplicate
  custom notification.

---

[Unreleased]: https://github.com/simonepelosi/vibez/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/simonepelosi/vibez/releases/tag/v0.0.1
