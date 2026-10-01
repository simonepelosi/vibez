# Contributing to vibez

Thanks for your interest in contributing! vibez is a TUI Apple Music player for Linux, macOS, and Windows.
This guide will help you set up a productive development environment.

---

## Project structure

```
vibez/
├── cmd/            # CLI entry-point (cobra, flags)
├── internal/
│   ├── assets/     # Embedded icon + desktop entry installation
│   ├── auth/       # Apple Music OAuth / MusicKit token flow
│   ├── config/     # JSON config (token, storefront, etc.)
│   ├── player/
│   │   ├── cdp/    # Chrome DevTools Protocol player (Playwright + Widevine)
│   │   ├── demo/   # In-memory fake player — no credentials required
│   │   └── webkit/ # WebKit + GStreamer fallback (30-s previews, Linux)
│   ├── player/mpris/ # Linux MPRIS D-Bus server
│   ├── provider/
│   │   ├── apple/  # Apple Music REST API provider
│   │   └── demo/   # In-memory fake provider — no credentials required
│   └── tui/        # Bubble Tea TUI: model, views, key bindings
├── scripts/        # Dev-token generator, helpers
└── web/            # MusicKit.js bridge (injected into headless Chrome)
```

---

## Running without Apple credentials (demo mode)

You **do not need** an Apple Developer account or Apple Music subscription to work on the UI.
The `--demo` flag loads a built-in fake player and provider with ten realistic tracks:

```sh
go run . --demo
```

All TUI interactions — search, queue, playback, keyboard shortcuts — work exactly as in production.  
The fake player advances the progress bar in real time and auto-advances to the next track.

---

## macOS setup

Install Google Chrome before testing Apple Music playback on macOS:

```sh
brew install --cask google-chrome
```

Demo mode does not require Chrome, Apple credentials, or network access.

## Windows setup

Use Go 1.26+ and installed Google Chrome. No CGo toolchain or WSL is required:

```powershell
go build -o vibez.exe .
.\vibez.exe --demo
.\vibez.exe --local --music-dir "$env:USERPROFILE\Music"
go test ./...
```

Install Node.js 22+ to run the local-audio JavaScript tests; they are also invoked by `go test`. Chrome is needed for real playback, not the unit tests. Use Windows Terminal for the interactive TUI. Repository text files use LF line endings on every platform.

The Windows config path is `%APPDATA%\vibez\config.json`; the browser driver cache is `%LOCALAPPDATA%\vibez\driver`. `VIBEZ_CHROME_PATH` or `CHROME_PATH` can select a custom Chrome executable. Local startup reports browser setup progress before opening the TUI. Installed Chrome sessions retain Chromium's sandbox on Windows and macOS.

For Apple Music testing without your own MusicKit key, coordinate with a maintainer on a test build after review of the exact PR head and the workflow's security. The current **Dev Build** executes PR code with Apple signing secrets available, so manual dispatch is not a safe shortcut for unreviewed code. Its `windows-amd64` artifact embeds a developer token; the private signing key must not be shared with contributors. Windows sign-in and full-track playback remain an end-to-end verification requirement even after build, demo, local-audio, and Widevine-capability checks pass.


## Full development setup (Apple credentials required)

Full-track streaming requires a valid MusicKit developer token and an Apple Music subscription for the Apple ID you use. Maintainer-built releases embed the developer token; contributors can use a maintainer-built Dev Build or an authorized short-lived token in their config.

Generating your own developer token instead requires Apple Developer Program membership and a MusicKit identifier/private key in your developer account.

### Generate a developer token

```sh
go run ./scripts/gen-devtoken -write
```

Set `APPLE_KEY_ID`, `APPLE_TEAM_ID`, and `APPLE_PRIVATE_KEY` in the environment first.
With `-write`, the token is saved to the platform config path (`~/.config/vibez/config.json`, or `%APPDATA%\vibez\config.json` on Windows). Without `-write`, it is printed to stdout; do not paste signing keys into issue comments or logs.

### First run

```sh
go run .
```

On Linux, Chrome (~150 MB, Widevine-enabled) is downloaded on first launch to `~/.cache/vibez/chrome`, and the Playwright driver is stored in `~/.cache/vibez/driver`.
On macOS and Windows, vibez uses installed Google Chrome; Windows stores the driver in `%LOCALAPPDATA%\vibez\driver`.
Your Apple ID is authorised in a popup browser window; the user token is cached in the config file.

---

## Building

```sh
# Standard build (uses embedded developer token if present)
make build

# Build with your own token baked in (for local testing)
APPLE_KEY_ID=... APPLE_TEAM_ID=... APPLE_PRIVATE_KEY="$(cat AuthKey.p8)" make build-with-token
```

---

## Running tests

```sh
go test ./...
```

Most tests run without credentials. The demo packages provide realistic test fixtures.

---

## Code style

- Standard `gofmt` / `goimports` formatting
- `golangci-lint run` must pass — the CI pipeline enforces this
- Keep functions small and focused; prefer explicit error propagation over panics
- Add a comment only when the _why_ is non-obvious; avoid commenting the _what_

---

## Submitting a PR

1. Fork the repository and create a feature branch
2. Write or update tests where appropriate
3. Run `go build ./...` and `go test ./...` locally
4. Open a PR — describe **what** you changed and **why**
5. For large features, open an issue first to discuss the approach

We review PRs as soon as we can. Feel free to ping in the issue thread if you don't hear back within a week.
