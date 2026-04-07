# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- **Lyrics panel (`y`)** — press `y` to open a full-width lyrics panel for the
  currently playing track. Lyrics are fetched automatically from
  [LRCLIB](https://lrclib.net), a free community-maintained database that requires
  no API key or account.
  - **Synced lyrics**: when timing data is available the current line is highlighted
    and the view auto-scrolls to keep it centred as the song progresses.
  - **Plain lyrics** fallback for tracks where timing data is unavailable.
  - Instrumental tracks are recognised and displayed as such.
  - Manual scroll with `j`/`k`; jump to top/bottom with `g`/`G`.
  - Fetching is non-blocking and stale results (e.g. from a quickly skipped track)
    are silently discarded.

### Changed
- **Discovery mode: two-step activation flow** — discovery is now configured and
  started separately, giving finer control over what gets queued:
  - Press **`d`** to open a metric picker in the Vibe panel. Use **`↑`/`↓`** (or
    `k`/`j`) to choose a similarity level — *same artist*, *similar artists*, *same
    genre*, *exploring*, or *pure discovery* — then **`Enter`** to confirm. The
    selected metric is stored and persists until changed; pressing `d` or `esc`
    cancels without changing the current setting.
  - Use the command palette (**`:`**) to actually start discovery:
    - `:discover <n>` — one-shot mode: queues exactly *n* songs based on the selected
      metric, then stops. Useful for a quick burst of new tracks.
    - `:discover auto` — continuous mode: keeps refilling the queue with one song
      whenever the last queued track starts playing (the previous default behaviour).
  - The Vibe panel now shows the active mode (*n songs* or *auto*) alongside the
    similarity bar when discovery is running.
  - The `+`/`-` keys still adjust the similarity level while discovery is active.

### Fixed
- **Discovery: library tracks queued with library ID** — `PlaybackID` now returns
  the library ID (`i.XXXXX`) for tracks the user owns, instead of the catalog ID.
  The catalog copy of a track may be CONTENT_RESTRICTED (e.g. due to regional
  restrictions or explicit-content settings) even when the user already has the
  track in their library; using the library ID avoids the restriction entirely.

### Improved
- **Debug log coverage** — the debug-log view now surfaces many previously silent
  operations:
  - JS: track changes (`nowPlayingItemDidChange`), `setQueue`/`setPlaylist` resolved
    counts, queue `remove`/`move`/`clear`, seek target, volume %, repeat mode
    (off/one/all), and shuffle on/off.
  - Go (CDP): storefront detected from MusicKit is now logged as `[storefront] <id>`.
  - Go (model): `[search] "query"…` on debounce fire and `[search] N track(s), M
    album(s), P playlist(s)` on result; `[player] playing` / `[player] paused` on
    playback state transitions.

---

## [0.0.3] — 2026-04-09

### Fixed
- **Discovery: unavailable / CONTENT_RESTRICTED tracks** — when Apple Music marks a
  discovery track as unavailable or restricted, vibez now:
  - receives a `goSkipped` notification from the JavaScript layer and records the
    track ID in a per-session blacklist so it is never proposed again;
  - purges the entire current queue of any blacklisted entries (not just the one that
    just triggered the notification);
  - filters blacklisted and already-queued IDs out of incoming search results before
    they are added to the queue (handles races where a track is blacklisted while a
    search is in flight, and prevents duplicate proposals);
  - immediately re-arms discovery so a fresh replacement is fetched without
    interrupting playback.
- **Discovery: stricter streamability filter** — the catalog search now only keeps
  tracks where `extendedAssetUrls.plus` is present. This field specifically indicates
  that a song can be streamed with an Apple Music subscription; the other URL fields
  (`hlsMediaPlaylist`, `enhancedHls`, `lightTunnel`) can be present for purchase-only
  tracks that would still fail at playback time.
- **Discovery: deduplication against the current queue** — discovery searches now
  snapshot the full queue at call time and exclude any track (by ID or artist/title)
  that is already queued, preventing the same song from being proposed twice.
- **Discovery: circuit breaker** — if discovery cannot find a fresh playable candidate
  after 5 consecutive retries it stops re-arming itself and logs a notice, preventing
  an infinite loop in edge cases where the search consistently returns blocked content.
- **Discovery trigger timing** — the background search now fires as soon as the last
  item in the queue starts playing, instead of 30 seconds before the end of any track.
  This gives discovery the maximum possible time to find a replacement before the queue
  runs dry. Trigger timing will be fully configurable in a future release.
- **Command-mode tab crash** — pressing `tab` after navigating the suggestion list and
  then narrowing the query no longer panics with `index out of range`.

---

## [0.0.2] — 2026-04-02

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
- Animated glow title in the README — three-layer SVG that mirrors the TUI's
  breathing lavender glow animation.
- **Install script** (`scripts/install.sh`): one-liner installer à la rustup —
  detects the platform, downloads the latest release from GitHub, verifies the
  SHA-256 checksum, installs to `~/.local/bin/` (overridable via
  `VIBEZ_INSTALL_DIR`), patches the shell profile (bash/zsh/fish) if the
  install dir is not yet in `$PATH`, and skips the download entirely if the
  installed version is already up to date.
- Unit tests for `extractDeb`: synthetic `.deb` fixtures built in-process
  (Go `archive/tar` + `compress/gzip` + hand-written ar writer), covering
  basic extraction, control.tar skipping, multi-file payloads, invalid magic,
  and missing `data.tar.*`.

### Changed
- README redesigned: centered logo, shields.io badges (CI, release, Go version,
  license), concise feature list, cleaner section layout.
- CI badge now tracks the `ci.yml` test workflow instead of `release.yml`.
- GoReleaser version pinned to `~> v2` (silences deprecation warning).
- **Search quality**: catalog search now goes through `amp-api.music.apple.com`
  (same endpoint used by the Apple Music web player and Cider), which returns
  `extendedAssetUrls` in results. Songs without streaming URLs — purchase-only
  or region-locked tracks — are filtered out before they can appear in the list.
- **Search debounce**: the API call now fires only after 400 ms of typing
  inactivity. Intermediate keystrokes are discarded via a generation counter,
  so rapid typing no longer triggers multiple parallel searches.
- **Flatpak Chrome extraction**: replaced `dpkg-deb` (unavailable in the
  GNOME Platform sandbox) with a pure-Go `ar(1)` parser + system `tar`.
  Chrome no longer re-downloads on every launch inside Flatpak.

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
