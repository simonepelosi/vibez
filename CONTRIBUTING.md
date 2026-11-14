# Contributing to vibez

Thanks for your interest in contributing! **vibez** is an open-source terminal Apple Music & local audio player written in Go for **Linux**, **macOS**, and **Windows**.

---

## Project Structure

```
vibez/
├── cmd/                        # Cobra CLI entry-point, flags, platform wiring
├── internal/
│   ├── assets/                 # Embedded app icon & desktop entry installer
│   ├── audioquality/           # Audio quality tiers & catalog variants
│   ├── auth/                   # Localhost MusicKit OAuth callback & token broker client
│   ├── config/                 # JSON config load/save (atomic writes, defaults)
│   ├── discord/                # Discord Rich Presence IPC client (Unix socket / named pipe)
│   ├── lastfm/                 # Optional Last.fm scrobbler & auth flow
│   ├── lyrics/                 # LRCLIB synced lyrics client & LRC parser
│   ├── openurl/                # Validated OS browser launcher
│   ├── player/
│   │   ├── browserless/        # In-process Widevine CDM C++ bridge, MP4 decryptor, HLS streamer & native sinks
│   │   ├── demo/               # In-memory fake player for --demo mode
│   │   ├── gst/                # Linux GStreamer pipeline wrapper & ADTS parser
│   │   ├── local/              # Offline audio player (GStreamer / Core Audio / Media Foundation)
│   │   ├── mpris/              # Linux MPRIS D-Bus server & client
│   │   └── smtc/               # Windows System Media Transport Controls bridge
│   ├── provider/
│   │   ├── apple/              # Apple Music REST API provider
│   │   ├── demo/               # Fake catalog provider for --demo mode
│   │   └── local/              # Recursive metadata scanner & lossless ISRC/tag matcher
│   ├── tui/
│   │   ├── model.go            # Bubble Tea v2 root model, keymaps, command palette
│   │   ├── art/                # Dithered artwork renderer & bounded image decoder
│   │   ├── styles/             # Lipgloss palettes & JSON custom theme loader
│   │   └── views/              # Panels (Library, Queue, Search, Lyrics, Feed, EQ, Vibe, Bear)
│   ├── updater/                # GitHub Release self-updater with SHA-256 checksum verification
│   ├── version/                # Build-injected version metadata
│   └── vibe/                   # Vibe planner (Ollama / LM Studio / llama.cpp / KeywordAgent)
├── flatpak/                    # Flatpak manifest & desktop files
├── scripts/
│   ├── gen-devtoken/           # MusicKit developer JWT generator CLI
│   └── install.sh              # Linux & macOS release installer
├── web/
│   └── login.html              # Browser OAuth page served on 127.0.0.1 during `vibez auth login`
└── worker/                     # Cloudflare Worker developer token broker & Node tests
```

---

## Running Without Apple Credentials (`--demo` & `--local`)

You **do not need** an Apple Developer account or Apple Music subscription to work on the TUI, search UX, theming, queue logic, local audio playback, or Lyrics/Vibe integrations:

```sh
# Built-in mock provider + mock player (10 sample tracks, simulated playback)
go run . --demo

# Real local audio playback without an Apple account
go run . --local --music-dir ~/Music
```

---

## Platform Development Setup

vibez has zero Chromium, Playwright, or WebKit dependencies. Native audio playback and the in-process Widevine CDM bridge use CGo (`CGO_ENABLED=1`), while pure-Go packages and `--demo` can also be verified with `CGO_ENABLED=0` on non-Linux hosts.

### Linux

Install Go 1.26+ plus GStreamer development headers and audio plugins:

```sh
# Debian / Ubuntu
sudo apt-get update && sudo apt-get install -y \
  pkg-config libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
  gstreamer1.0-plugins-good gstreamer1.0-libav
```

On Arch or Fedora, install the corresponding `gstreamer`, `gst-plugins-base`, `gst-plugins-good`, and `gst-libav` (plus `gst-plugins-bad` if testing 24/32-bit AIFF files).

### macOS

Install Go 1.26+ and Xcode Command Line Tools (`clang` + `AudioToolbox`/`CoreAudio` system frameworks):

```sh
xcode-select --install
go build ./...
go test ./...
```

### Windows

Install Go 1.26+ and **MinGW-w64** (`x86_64-w64-mingw32-gcc` / `g++`) so `CGO_ENABLED=1` compiles the C++ Widevine CDM bridge, Media Foundation / `waveOut` sinks, and SMTC integration:

```powershell
# Full native build & tests (with MinGW-w64 on PATH)
$env:CGO_ENABLED = "1"
go build -o vibez.exe .
go test ./...

# Pure-Go checks (works without MinGW)
$env:CGO_ENABLED = "0"
go test ./...
```

Repository text files use **LF** line endings on every platform (enforced by `.pre-commit-config.yaml`).

---

## Testing Full Apple Music Playback

Full-track streaming requires:
1. An Apple Music subscription on the Apple ID you log in with (`vibez auth login`)
2. A valid MusicKit developer token (release/Dev Builds fetch short-lived tokens from the Cloudflare token broker in `worker/`, whereas local source builds use your own MusicKit key or an authorized dev token)

Maintainers can also trigger the **Dev Build** GitHub Actions workflow (`dev-build.yml`), which runs exclusively from the default branch and embeds a short-lived developer token without exposing signing keys to PR code.

### Using your own Apple MusicKit key

Export `APPLE_KEY_ID`, `APPLE_TEAM_ID`, and `APPLE_PRIVATE_KEY` (or place them in a gitignored `.env`, optionally pointing `APPLE_PRIVATE_KEY_FILE` at your `.p8` file):

```sh
# Write a fresh developer JWT to config.json (~30 day lifetime)
make refresh-token

# Or build and install with an embedded developer token
make build-with-token
make install
```

Never commit `.p8` signing keys, tokens, or `.env` files, and never paste secrets into issues or PR comments.

### Widevine CDM discovery

On launch, the browserless player searches `VIBEZ_CDM_PATH` and standard local Chrome/Chromium/Edge/Brave/Firefox directories. If none is found, it downloads the standalone official Widevine CDM component into:
- Linux: `~/.cache/vibez/cdm`
- macOS: `~/Library/Caches/vibez/cdm`
- Windows: `%LOCALAPPDATA%\vibez\cdm`

Live network CDM download tests are skipped unless `VIBEZ_TEST_NETWORK=1` is set.

---

## Running Tests, Linting & Pre-commit

CI runs across Linux, macOS, Windows (`CGO_ENABLED=0` and `CGO_ENABLED=1`), and `pre-commit` on Ubuntu:

```sh
# Run tests (with race detector on supported hosts)
go test -race ./...

# Vet and lint (uses .golangci.yml, including gosec & security rules)
go vet ./...
golangci-lint run --config=.golangci.yml --fix=false

# Run Worker tests if touching worker/
node --test worker/test/token.test.mjs

# Run all configured pre-commit checks
pre-commit run --all-files
```

---

## Code Style & Security Guidelines

- Standard `gofmt` formatting and clean modular helpers (keep functions concise and avoid duplicated logic)
- `golangci-lint` (`.golangci.yml`) and `pre-commit` must pass without suppression unless explicitly justified
- Never trust remote strings in TUI/logs/D-Bus metadata: sanitize with `provider.Clean`, validate upstream URLs (`https://` or loopback HTTP), bound `io.Reader` / image decodes, and avoid leaking `Authorization` or user tokens across HTTP redirects
- Add or update unit/fuzz tests alongside behavioral or security fixes
- Comment the non-obvious *why*, not the *what*

---

## Submitting a PR

1. Fork the repository and branch from `main`
2. Make focused changes with tests covering new behavior or bug fixes
3. Run `go vet ./...`, `golangci-lint run`, and `go test -race ./...` locally
4. Open a pull request describing **what** changed, **why**, and how you verified it
5. For major features or new music providers, open an issue first to coordinate design
