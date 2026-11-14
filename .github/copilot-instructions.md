# vibez — Copilot instructions

vibez is a terminal Apple Music and local audio player written in Go. It streams Apple Music directly via an in-process Widevine CDM and native audio sinks (GStreamer on Linux, Core Audio on macOS, Media Foundation + `waveOut` on Windows), supports offline `--local` / `--hybrid` lossless playback, and renders a TUI with Bubble Tea v2.

## Build & test

```bash
# Required system libs & GStreamer plugins (Ubuntu/Debian)
sudo apt-get install -y pkg-config libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev gstreamer1.0-plugins-good gstreamer1.0-libav

# Build, lint, and test
go build ./...
go vet ./...
golangci-lint run --config=.golangci.yml --fix=false
go test -race ./...

# Windows native target requires MinGW-w64 (CGO_ENABLED=1); pure-Go checks work with CGO_ENABLED=0
CGO_ENABLED=0 GOOS=windows go build ./...
CGO_ENABLED=0 GOOS=darwin go build ./...

# Run without Apple credentials
go run . --demo
go run . --local --music-dir ~/Music
```

## Architecture

```
cmd/                                  # Cobra CLI flags, local/demo/browserless flow wiring
internal/tui/
  ├── model.go                        # Bubble Tea v2 model, keymaps, command mode, waitForState()
  ├── art/                            # Dithered Unicode artwork renderer & bounded image decoder
  ├── styles/                         # Built-in & custom JSON theme loader
  └── views/                          # Search, Library, Queue, Lyrics, Feed, EQ, Vibe, Now Playing, Bear
internal/player/
  ├── browserless/                    # Native Apple Music player + StreamServer (authenticated loopback HTTP)
  │   ├── cdm/                        # CGo bridge (cdm_bridge.cpp) to in-process Widevine CDM (Host_11)
  │   ├── license/client.go           # Apple MZPlay webPlayback & Widevine license HTTP client
  │   ├── mp4/decryptor.go            # fMP4 cenc sample decryption & ADTS AAC packaging
  │   ├── rawformat.go                # Raw upload (.mp3/.m4a) sniffing & duration estimation
  │   ├── sink_darwin.cpp             # Core Audio native sink
  │   └── sink_windows.cpp            # Media Foundation + waveOut native sink
  ├── gst/                            # Linux GStreamer pipeline + vibezaacparse ADTS parser
  ├── local/                          # Offline player (GStreamer / Core Audio / Media Foundation)
  ├── mpris/ & smtc/                  # Linux MPRIS D-Bus & Windows SMTC media controls
  └── demo/                           # In-memory fake player
internal/provider/
  ├── apple/                          # Apple Music catalog & library REST API provider
  ├── local/                          # Recursive local scanner & ISRC/metadata lossless matcher
  └── demo/                           # In-memory fake catalog
internal/vibe/                        # Vibe planner (Ollama / OpenAI-compat / KeywordAgent) & resolver
internal/lyrics/                      # LRCLIB synced/plain lyrics client & LRC parser
internal/auth/, config/, discord/, lastfm/, updater/
```

## Key files

| File | Role |
|------|------|
| `internal/player/browserless/player.go` | Native Apple Music streaming & hybrid local substitution |
| `internal/player/browserless/streamer.go` | Loopback HTTP progressive AAC/raw uploader server |
| `internal/player/browserless/cdm/cdm_bridge.cpp` | In-process Widevine CDM C++ Host_11 bridge |
| `internal/provider/local/matcher.go` | Lossless local matcher for `--hybrid` mode |
| `internal/vibe/planner.go` | On-device LLM + keyword mood planner |
| `internal/tui/model.go` | TUI model, panels, key handling, commands |
| `internal/tui/model_test.go` | Core TUI test suite |

## Bubble Tea v2 test format

```go
// Key press (v2 format)
tea.KeyPressMsg{Code: 'n', Text: "n"}
tea.KeyPressMsg{Code: tea.KeyEsc}
```

## Conventions & pitfalls

- The TUI renderer runs independently at frame rate; `View()` must avoid unnecessary allocations and blocking I/O.
- Sanitize all untrusted strings displayed in the TUI, logs, or D-Bus/IPC metadata using `provider.Clean` (strip ANSI/control/BiDi characters).
- Enforce `https://` (or loopback `http://` where required for local loopback servers/tests) and bound HTTP response body / image reads.
- Every package test suite must remain runnable headless without an X/Wayland display or Apple Music credentials (`go test -race ./...`).
