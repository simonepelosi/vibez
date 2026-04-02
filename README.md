# ♪ vibez

> **vibe-driven music player for your terminal**

vibez is an open-source TUI (terminal user interface) Apple Music player for Linux. It streams full tracks directly from Apple Music — no external player required — and lets you search, browse, queue, and control playback entirely from the keyboard.

Playback is powered by an embedded Chrome instance with Widevine DRM (auto-downloaded via Playwright), falling back to WebKit+GStreamer (30-second previews) when Chrome is unavailable. MPRIS is registered so desktop media controls and notifications show "vibez" as the player.

---

## Features

- 🎵 Browse your Apple Music library (playlists, albums, tracks)
- 🔍 Real-time search of the Apple Music catalog
- 🎶 Full-track streaming via Chrome + Widevine DRM (no external player needed)
- 📋 Queue management — add songs with `tab`, navigate with `n`/`p`
- 🐻 Animated bear mascot — sleeps when idle, dances when music plays
- ⏳ Pulsing spinner (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`) when loading or buffering
- 🖥️  MPRIS D-Bus registration — desktop media keys and notifications work out of the box
- ⌨️  Fully keyboard-driven TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- 🔌 Extensible provider architecture (Spotify, YouTube Music planned)

---

## Requirements

- **Linux** (x86-64 or arm64)
- **Go 1.22+**
- **Apple Developer Account** with a MusicKit key (for full-track streaming)
- **Chrome** — downloaded automatically (~150 MB, one-time) to `~/.cache/vibez/playwright` via Playwright; no system install needed
- **webkit2gtk-4.0** — only needed for the WebKit fallback mode (30 s previews)

> **No Cider, no VLC, no external music app.** vibez streams Apple Music directly.

---

## Installation

### Flatpak (recommended — any Linux distro)

Download the latest `vibez.flatpak` bundle from the [Releases](https://github.com/simonepelosi/vibez/releases) page, then install it with:

```bash
flatpak install --user vibez.flatpak
flatpak run io.github.simonepelosi.vibez
```

> **First run:** Chrome (~150 MB) is downloaded automatically to your Flatpak cache.  
> The Flatpak bundles all native dependencies (WebKitGTK, GStreamer) — no system libraries required.

### Homebrew (Linux)

```bash
brew install simonepelosi/tap/vibez
```

### From source

```bash
git clone https://github.com/simonepelosi/vibez
cd vibez
make build-with-token   # requires APPLE_KEY_ID / APPLE_TEAM_ID / APPLE_PRIVATE_KEY
```

---

## Configuration

vibez stores its configuration at `~/.config/vibez/config.json`.
The file is created automatically on first run with sensible defaults.

```json
{
  "apple_developer_token": "",
  "apple_user_token": "",
  "apple_key_id": "",
  "apple_team_id": "",
  "storefront": "us",
  "auth_port": 7777,
  "provider": "apple",
  "theme": "default"
}
```

### Getting an Apple Developer Token

1. Go to [developer.apple.com](https://developer.apple.com/account/resources/authkeys/list)
2. Create a new key with the **MusicKit** capability
3. Download the `.p8` private key file
4. Note your **Key ID** and **Team ID**
5. Run the bundled helper to generate a signed JWT:

```bash
go run ./scripts/gen-devtoken \
  --key-id   <KEY_ID>   \
  --team-id  <TEAM_ID>  \
  --key-file <path/to/AuthKey_XXXXXX.p8>
```

6. Paste the output into `apple_developer_token` in `~/.config/vibez/config.json`

---

## Usage

### Log in to Apple Music

```bash
vibez auth login
```

On first run Chrome opens a login window. Sign in with your Apple ID and the user token is saved automatically.

### Check auth status

```bash
vibez auth status
```

### Log out

```bash
vibez auth logout
```

### Open the TUI

```bash
vibez
```

### Print version

```bash
vibez version
```

---

## TUI Key Bindings

### Global

| Key | Action |
|-----|--------|
| `space` | Play / Pause |
| `n` | Next track |
| `p` | Previous track |
| `+` / `=` | Volume up |
| `-` | Volume down |
| `r` | Cycle repeat mode (off → all → one) |
| `s` | Toggle shuffle |
| `/` | Open search |
| `l` | Toggle library panel |
| `q` | Toggle queue panel |
| `:q` | Quit |
| `ctrl+c` | Quit |

### Search mode (`/`)

| Key | Action |
|-----|--------|
| *(type)* | Filter results in real time |
| `↑` / `↓` | Navigate results |
| `enter` | Play now (replaces current queue) |
| `tab` | Add to queue (keeps playing) |
| `esc` | Close search |

### Library panel (`l`)

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate list |
| `enter` | Open playlist / play track |
| `tab` | Switch tab (Playlists / Albums / Tracks) |
| `esc` | Back / close panel |

---

## Audio Engines

vibez auto-selects the best available engine at startup and prints which one it chose:

| Engine | Tracks | How it works |
|--------|--------|--------------|
| **Chrome + Widevine** *(primary)* | Full tracks | Playwright launches a headless Chrome; MusicKit JS streams via Widevine DRM |
| **WebKit + GStreamer** *(fallback)* | 30 s previews | Embedded webkit2gtk-4.0 webview; GStreamer decodes preview URLs |

Chrome is downloaded once to `~/.cache/vibez/playwright` and reused on every subsequent start.

---

## Architecture

```
vibez/
├── cmd/                    # CLI commands (cobra): root, auth, version
├── internal/
│   ├── config/             # Config file management
│   ├── auth/               # MusicKit JS OAuth flow (local web server)
│   ├── provider/           # Provider interface + Apple Music implementation
│   ├── player/
│   │   ├── player.go       # Player interface + State type
│   │   ├── cdp/            # Chrome CDP player (primary — full Widevine tracks)
│   │   ├── webkit/         # WebKit player (fallback — 30 s previews)
│   │   ├── gst/            # GStreamer audio decoder (used by WebKit mode)
│   │   └── mpris/          # MPRIS D-Bus server (desktop integration)
│   ├── tui/
│   │   ├── model.go        # Bubble Tea model, key handling, layout
│   │   ├── views/          # Search, queue, library, bear mascot, now-playing
│   │   └── styles/         # Colour palette and lipgloss styles
│   └── vibe/               # Vibe agent: mood → search query
├── scripts/
│   └── gen-devtoken/       # Helper to generate Apple MusicKit JWT
└── web/                    # Embedded HTML for auth login page
```

The **provider** interface makes it easy to add new music services:

```go
type Provider interface {
    Search(ctx context.Context, query string) (*SearchResult, error)
    GetLibraryTracks(ctx context.Context) ([]Track, error)
    GetLibraryPlaylists(ctx context.Context) ([]Playlist, error)
    // ...
}
```

The **player** interface abstracts playback control:

```go
type Player interface {
    Play() error
    Pause() error
    Next() error
    Previous() error
    SetQueue(ids []string) error
    AppendQueue(ids []string) error
    // ...
}
```

---

## Roadmap

- [x] Queue management (add, navigate, auto-advance)
- [ ] **Spotify** provider (OAuth2 + Web API)
- [ ] **YouTube Music** provider
- [ ] LLM-powered vibe agent (OpenAI / Ollama)
- [ ] Lyrics display
- [ ] Last.fm scrobbling
- [ ] Notification support (desktop popups on track change)

---

## Contributing

Contributions are welcome! Please open an issue to discuss your idea before sending a PR.

```bash
git clone https://github.com/simonepelosi/vibez
cd vibez
go mod tidy
go build ./...
go test ./...
```

Pre-commit hooks run `go build`, `go vet`, `go test`, `golangci-lint`, and the MusicKit JS test suite. Install them with:

```bash
pip install pre-commit
pre-commit install
```

---

## License

MIT © Simone Pelosi
