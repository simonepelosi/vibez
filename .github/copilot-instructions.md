# vibez — Copilot instructions

vibez is a terminal Apple Music player written in Go. It streams via MusicKit JS
running in a headless Chromium instance (CDP), and renders a TUI with Bubbletea v2.

## Build & test

```bash
# Required system libs (Ubuntu/Debian)
sudo apt-get install -y libwebkit2gtk-4.1-dev libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev

# Always set PKG_CONFIG_PATH when building or testing
PKG_CONFIG_PATH=$PWD/pkg-config go build ./...
PKG_CONFIG_PATH=$PWD/pkg-config go test ./...
```

## Architecture

```
TUI (Bubbletea v2)
  └── model.go — all player state, key handling, rendering
        └── waitForState() — drains channel, returns latest state

CDP player (internal/player/cdp/player.go)
  └── applyState()  — JSON → player.State → fan-out to subscriber channels
  └── dispatch()    — fire-and-forget goroutine (never block inside)
  └── Subscribe()   — returns chan player.State (buffer=8)

musickit.html (internal/player/web/musickit.html)
  └── notifyState() — pushes state to Go via goPlayerStateChange binding
  └── _doPlayAt()   — queues and plays a track
  └── _stopAndWait() — waits only when mid-seek (states 6/7)
  └── setInterval(notifyState, 200) — unconditional 200ms polling
```

### JS → Go state flow

`goPlayerStateChange(JSON)` is a Playwright binding — async, non-blocking from Go's
perspective. This is the **only** safe way to push state from JS to Go at any frequency.

### CRITICAL: never call `page.Evaluate` at high frequency

`page.Evaluate` in Playwright-Go is **synchronous and blocking**: it serialises ALL
CDP traffic over a single WebSocket. Calling it faster than ~2 Hz will starve the
`goPlayerStateChange` callbacks, causing a deadlock that can freeze the entire machine.

**Rule:** all state polling must stay inside JavaScript via `setInterval`. Go only
receives on the subscription channel.

## MusicKit playback states

| Value | Name      | Notes                          |
|-------|-----------|--------------------------------|
| 0     | none      |                                |
| 1     | loading   | → Go `Loading=true`            |
| 2     | playing   |                                |
| 3     | paused    |                                |
| 4     | stopped   |                                |
| 5     | ended     |                                |
| 6     | seekFwd   | → Go `Loading=true`; CE risk   |
| 7     | seekBwd   | → Go `Loading=true`; CE risk   |
| 8     | waiting   | → Go `Loading=true`            |
| 9     | completed | natural auto-advance           |
| 10    | completed | natural auto-advance           |

**CE risk**: calling `setQueue` during states 6 or 7 triggers a spurious
`CONTENT_EQUIVALENT` error. `_stopAndWait` guards against this.

## Key files

| File | Role |
|------|------|
| `internal/player/web/musickit.html` | JS player; all MusicKit state management |
| `internal/player/cdp/player.go` | CDP/Playwright Go side; state delivery |
| `internal/tui/model.go` | TUI model; key handling, rendering |
| `internal/tui/model_test.go` | Full test suite |

## Bubbletea v2 test format

```go
// Key press (v2 format)
tea.KeyPressMsg{Code: tea.KeyRune, Text: "n"}
tea.KeyPressMsg{Code: tea.KeyEsc}
```

## Common pitfalls

- **Do not** add `setInterval` calls from Go (via `page.Evaluate`) — use JS `setInterval` only.
- **Do not** await `_stopAndWait` for completed/playing/paused states — only needed for seekFwd/seekBwd.
- When adding new CDP bindings, always use `dispatch()` (fire-and-forget) on the Go side.
- The TUI renderer runs at 60 FPS independently; `View()` must stay under ~1ms.
