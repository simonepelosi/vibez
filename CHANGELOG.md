# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- **Custom themes** — set `"theme"` in `~/.config/vibez/config.json` to switch palette.
  Four built-in themes ship out of the box: `default` (Tokyo Night / Catppuccin Mocha),
  `dracula`, `gruvbox`, and `nord`. Custom palettes can be created as JSON files in
  `~/.config/vibez/themes/<name>.json`; any missing or invalid color fields fall back to
  the default theme automatically. All 20+ color roles are themeable: core palette,
  semantic accents, bear mascot, progress-bar gradient, glow animation, and mode chips.
  Closes #14.

---

## [0.0.9] — 2026-04-30

### Fixed
- **Library panel (`l`) not activating** — reopening the library panel after the
  music engine was ready replaced the internal model pointer instead of updating it
  in-place, causing the active-panel check to always fail silently. The panel state
  is now updated in-place so the pointer identity is preserved.
- **Library panel rendering blank** — the `WindowSizeMsg` sized the original inner
  model before the engine was ready; when the engine started a new model was created
  with zero dimensions. The panel now re-applies the current window dimensions after
  the engine is ready, and also re-sizes lazily on every render (consistent with the
  queue panel).
- **Apple Music API 400 on track loading** — all four track-fetching endpoints sent
  `limit=300`, which exceeds the Apple Music API maximum of 100. Corrected to
  `limit=100` across `GetPlaylistTracks`, `GetAlbumTracks`, `GetLibraryAlbumTracks`,
  and `GetCatalogPlaylistTracks`.

### Changed
- **Library view simplified to Playlists only** — the Albums and Tracks tabs have
  been removed. The library panel now shows only the user's playlists, keeping the
  interface focused and reducing unnecessary API calls.
- **Library and drill-down errors go to debug log** — playlist load errors and
  track-fetch errors are no longer shown as inline messages in the library panel;
  they are written to the debug-logs view (`d` key) so the UI stays clean.

---

## [0.0.8] — 2026-04-18

### Added
- **Last.fm scrobbling** — optionally connect your Last.fm account with
  `vibez auth lastfm login` and vibez will scrobble your plays automatically.
  Now Playing updates are sent when a track starts; a scrobble is submitted
  once you've listened to at least half the track (or 4 minutes, whichever
  comes first). Tracks under 30 seconds are ignored, pauses don't count.
- **Seek** — press `←` / `→` to jump backward or forward 10 seconds in the
  current track. Use `:seek <seconds>` to jump to an absolute position.
  Closes #9.

- **Search: albums and playlists** — the search popup (`/`) now returns all three
  result types in a unified scrollable list, grouped into **Tracks**, **Albums**,
  and **Playlists** sections. Each section only appears when the provider returns
  results for it.
  - Album rows show `[album]` tag, artist name, and track count (when available).
  - Playlist rows show `[playlist]` tag and track count (when available).
  - Press **`Enter`** on an album or playlist to fetch its tracks and play them
    immediately (replaces the current queue), identical behaviour to tracks.
  - Press **`Tab`** on an album or playlist to fetch its tracks and append them
    to the queue without interrupting playback.
  - Library playlists (`p.` IDs) are fetched via the library endpoint;
    catalog playlists use the catalog endpoint — the correct path is resolved
    automatically.
  - Library albums (`l.` IDs) are fetched via `/me/library/albums/{id}/tracks`;
    catalog albums (numeric IDs) use the catalog endpoint. This fixes the 404
    *"No related resources"* error that occurred when playing albums returned
    from the user's library by the search.
  - The footer hint line is context-sensitive: it reads *"play track / album /
    playlist"* and *"add track / album / playlist to queue"* depending on what
    is currently highlighted.
  - Navigation (`↑` / `↓` / `PgUp` / `PgDn`) moves through all sections
    continuously, skipping non-selectable section headers automatically.
  - Section headers are colour-coded: **Tracks** in blue, **Albums** in purple,
    **Playlists** in green, making each section immediately distinguishable at
    a glance.
- **Provider: `GetLibraryAlbumTracks`** — new method on the `Provider` interface
  for fetching tracks of a library album by its library ID. Supports pagination
  (follows `next` cursors) like the other library track endpoints.

### Fixed
- **Search: 404 on library album playback** — selecting an album whose ID starts
  with `l.` (i.e. present in the user's library) no longer hits the catalog
  endpoint and returns a 404. `fetchSearchCollectionCmd` now routes `l.` IDs to
  `GetLibraryAlbumTracks` and numeric IDs to `GetAlbumTracks`.
- **Albums and playlists now sourced from the correct API endpoint** — the
  catalog search was previously sent entirely to `amp-api.music.apple.com`, a
  web-player endpoint that reliably returns songs with `extendedAssetUrls` but
  does not return albums or playlists. As a result albums were silently sourced
  from the *library* search (`l.` IDs), which only contains tracks the user has
  explicitly added — a potentially incomplete subset of the full release.
  The search is now split into three concurrent requests:
  - **Library songs + playlists** — `api.music.apple.com/v1/me/library/search`
    (unchanged; library songs play guaranteed and user playlists are fully owned).
  - **Catalog songs** — `amp-api.music.apple.com` with `extend=extendedAssetUrls`
    so purchase-only / region-locked tracks can be filtered before reaching the
    queue (unchanged behaviour).
  - **Catalog albums + playlists** — new request to the standard
    `api.music.apple.com/v1/catalog/{sf}/search?types=albums,playlists` endpoint,
    which reliably returns full-catalogue albums with correct numeric IDs.
  Library albums are intentionally excluded from all search results: selecting an
  album in the search popup now always fetches every track on the release.
- **`GetAlbumTracks` now paginates** — previously only the first page was fetched,
  silently truncating albums whose track list spans multiple API pages. The
  function now follows `page.Next` cursors like the playlist fetchers. Page limit
  raised to 300 (API maximum) across all four track-fetching helpers to minimise
  round-trips for long releases.

---

## [0.0.7] — 2026-04-07

### Changed
- **Progress bar: static gradient zigzag** — replaced the flat `━●─` bar with a
  `╱╱╲╲` zigzag wave that spans the full width. The elapsed portion is coloured with
  a per-character blue → lavender → rose-pink gradient; the remaining portion uses
  the same zigzag in muted grey so the waveform reads as one continuous line. The
  pattern is static (no scrolling).
- **Slower bear & title glow animation** — the glow tick interval increased from
  200 ms to 500 ms, giving the bear mascot and the now-playing title a more relaxed
  breathing cadence.

---

## [0.0.6] — 2026-04-07

### Added
- **Scrollable mini-queue** — the queue panel on the main screen (left split) now
  scrolls with `j`/`k` (or arrow keys). Previously tracks beyond the visible area
  were silently clipped with no way to reach them.
- **Auto-scroll to current track** — when the playing track changes the mini-queue
  view automatically scrolls to centre the new track in the visible area.
- **Queue index and count** — each row in the mini-queue now shows its 1-based
  position (right-aligned to the digit width of the total, e.g. ` 3.`/`12.`). The
  panel header shows the total number of queued tracks (e.g. `Queue  12 tracks`).

### Fixed
- **Shuffle: actual queue reordering** — `s` now shuffles vibez's own `_q` array
  instead of setting MusicKit's internal `shuffleMode` flag. Because vibez calls
  `setQueue({songs:[id]})` one track at a time, MusicKit's native shuffle had nothing
  to operate on. The fix implements a Fisher-Yates shuffle over all tracks after the
  current index so playback is not interrupted.
  - Toggling shuffle **off** restores the original track order and resyncs the current
    position so next/prev history stays coherent.
  - Loading a new queue or playlist clears the shuffle snapshot so a fresh queue
    always starts in natural order.
- **Shuffle UI toggle** — the `⇄` control in the player was immediately reverting to
  its muted (inactive) style after being activated. The state poll emits
  `shuffleMode: m.shuffleMode`, so the MusicKit property now stays in sync with the
  toggle to keep the indicator correctly lit.

### Changed
- **CI: Flatpak jobs removed** — the `flatpak-prep` and `flatpak` pipeline jobs are
  disabled for now (deployment not active). The release workflow is a single lean job.

---

## [0.0.5] — 2026-04-07

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
- **Recommendations feed panel (`F`)** — press `F` to open a full-width feed panel
  showing personalised album and playlist recommendations from Apple Music.
  - Recommendations are grouped by curated category (e.g. *Recommended Albums*,
    *New Releases for You*) and loaded on first open.
  - Navigate with `j`/`k`; press `r` to refresh the feed at any time.
  - Press **`Enter`** on a highlighted item to play it immediately (replaces the
    current queue); press **`Tab`** to append its tracks to the queue instead.
  - Both albums and catalog playlists are supported.
- **Volume commands** — new command-palette entries for fine-grained volume control:
  - `:vol <0-100>` — set volume to an absolute level.
  - `:vol +n` / `:vol -n` — raise or lower volume by *n* percent.
  - `:vol` — show the current volume level in the status bar.
  - `:mute` — mute audio (run again to restore the previous volume). The header
    shows `🔇 muted` in place of the volume percentage while muted.

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

[Unreleased]: https://github.com/simonepelosi/vibez/compare/v0.0.7...HEAD
[0.0.7]: https://github.com/simonepelosi/vibez/compare/v0.0.6...v0.0.7
[0.0.6]: https://github.com/simonepelosi/vibez/compare/v0.0.5...v0.0.6
[0.0.5]: https://github.com/simonepelosi/vibez/compare/v0.0.4...v0.0.5
[0.0.4]: https://github.com/simonepelosi/vibez/compare/v0.0.3...v0.0.4
[0.0.3]: https://github.com/simonepelosi/vibez/compare/v0.0.2...v0.0.3
[0.0.2]: https://github.com/simonepelosi/vibez/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/simonepelosi/vibez/releases/tag/v0.0.1
