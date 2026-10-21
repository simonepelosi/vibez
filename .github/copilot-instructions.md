# vibez — Copilot instructions

vibez is a terminal Apple Music player written in Go. It streams directly via an
in-process Widevine CDM and native audio sinks (GStreamer on Linux, Core Audio on
macOS, Media Foundation + waveOut on Windows), and renders a TUI with Bubbletea v2.

## Build & test

```bash
# Required system libs & GStreamer plugins (Ubuntu/Debian)
sudo apt-get install -y libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev gstreamer1.0-plugins-good gstreamer1.0-libav

# Build and test
go build ./...
go test ./...

# Demo mode
go run . --demo
```

## Architecture

```
TUI (Bubbletea v2)
  └── model.go — all player state, key handling, rendering
        └── waitForState() — drains channel, returns latest state

Browserless player (internal/player/browserless/player.go)
  ├── license/client.go — Apple MZPlay webPlayback & Widevine license APIs
  ├── cdm/              — CGo bridge to in-process Widevine CDM (Host_11)
  ├── mp4/decryptor.go  — fMP4 cenc sample decryption & ADTS AAC packaging
  ├── streamer.go       — authenticated loopback HTTP progressive AAC server
  └── audioSink         — GStreamer (Linux), Core Audio (macOS), Media Foundation + waveOut (Windows)
```

## Key files

| File | Role |
|------|------|
| `internal/player/browserless/player.go` | Native Apple Music streaming player |
| `internal/player/browserless/streamer.go` | Progressive HLS segment decryption & ADTS loopback server |
| `internal/player/browserless/cdm/cdm_bridge.cpp` | In-process Widevine CDM C++ Host_11 bridge |
| `internal/tui/model.go` | TUI model; key handling, rendering |
| `internal/tui/model_test.go` | Full TUI test suite |

## Bubbletea v2 test format

```go
// Key press (v2 format)
tea.KeyPressMsg{Code: tea.KeyRune, Text: "n"}
tea.KeyPressMsg{Code: tea.KeyEsc}
```

## Common pitfalls

- The TUI renderer runs at 60 FPS independently; `View()` must stay under ~1ms.
