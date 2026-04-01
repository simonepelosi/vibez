# Contributing to vibez

Thanks for your interest in contributing! vibez is a TUI Apple Music player for Linux.  
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
│   │   └── webkit/ # WebKit + GStreamer fallback (30-s previews)
│   ├── player/mpris/ # MPRIS D-Bus server
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

## Full development setup (Apple credentials required)

Full-track streaming requires:

1. **Apple Developer Program membership** (~$99/year, [developer.apple.com](https://developer.apple.com))
2. A **MusicKit** identifier + private key in your developer account
3. An **Apple Music subscription** for the Apple ID you'll use

### Generate a developer token

```sh
go run ./scripts/gen-devtoken
```

Follow the prompts to paste your MusicKit private key and Key ID.  
The token is written to `~/.config/vibez/config.json`.

### First run

```sh
go run .
```

Chrome (~150 MB, Widevine-enabled) is downloaded on first launch to `~/.cache/vibez/playwright`.  
Your Apple ID is authorised in a popup browser window; the user token is cached in the config file.

---

## Building

```sh
# Standard build (uses embedded developer token if present)
make build

# Build with your own token baked in (for local testing)
APPLE_DEVELOPER_TOKEN=... make build-with-token
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
