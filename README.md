<p align="center">
  <img src="assets/logo.png" width="160" alt="vibez logo">
</p>

<p align="center">
  <img src="assets/title.svg" alt="♪ vibez" width="460">
</p>

<p align="center">
  <strong>Apple Music in your terminal. Vibe-driven. Keyboard-first.</strong>
</p>

<p align="center">
  <a href="https://github.com/simonepelosi/vibez/actions"><img src="https://img.shields.io/github/actions/workflow/status/simonepelosi/vibez/ci.yml?style=flat-square&label=CI" alt="CI"></a>
  <a href="https://github.com/simonepelosi/vibez/releases"><img src="https://img.shields.io/github/v/release/simonepelosi/vibez?style=flat-square" alt="Release"></a>
  <a href="https://github.com/simonepelosi/vibez/blob/main/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/simonepelosi/vibez?style=flat-square" alt="Go version"></a>
  <a href="https://github.com/simonepelosi/vibez/blob/main/LICENSE"><img src="https://img.shields.io/github/license/simonepelosi/vibez?style=flat-square" alt="License"></a>
  <a href="https://github.com/simonepelosi/vibez/releases"><img src="https://img.shields.io/github/downloads/simonepelosi/vibez/total?style=flat-square&label=downloads" alt="Downloads"></a>
  <a href="https://github.com/simonepelosi/vibez/stargazers"><img src="https://img.shields.io/github/stars/simonepelosi/vibez?style=flat-square" alt="Stars"></a>
</p>

<p align="center">
  If you enjoy vibez, consider supporting its development — it helps keep the project alive! ☕<br>
  <a href="https://ko-fi.com/pelpsi"><img src="https://img.shields.io/badge/☕_buy_me_a_coffee-donate-ff5e5b?style=for-the-badge" alt="Donate on Ko-fi"></a>
</p>

[Installation](#installation) · [Usage](#usage) · [Features](#features) · [Key Bindings](#key-bindings) · [Roadmap](#roadmap)

---

vibez is an open-source TUI Apple Music player for Linux. Search, queue, and control playback entirely from the keyboard — no Cider, no VLC, no external app.

Full tracks stream via an embedded headless Chrome with Widevine DRM (auto-downloaded). Falls back to WebKit + GStreamer (30 s previews) when Chrome is unavailable. MPRIS support means desktop media keys and notifications just work.

---

## Features

### 🎵 Music Playback

- **Full-track streaming** via headless Chrome + Widevine DRM — the real deal, not 30-second clips
- **Automatic fallback** to WebKit + GStreamer (30 s previews) when Chrome is unavailable
- **Playback controls** — play/pause, next, previous, volume up/down
- **Repeat modes** — cycle through off, repeat-all, and repeat-one
- **Shuffle** — randomise your queue with a single keypress

### 🔍 Apple Music Integration

- **Browse your library** — playlists, albums, and tracks all in one place
- **Real-time catalog search** — find any song, album, or artist from the full Apple Music catalog as you type
- **Secure authentication** — MusicKit OAuth flow via an embedded Chrome window

### 📋 Queue Management

- **Add tracks to queue** with `tab` from search or library
- **Navigate the queue** — jump to any track or let it auto-advance
- **Persistent queue panel** — toggle it on/off without losing your place

### 🖥️ System Integration

- **MPRIS D-Bus** — your desktop media keys (play, pause, next, previous) work out of the box
- **Desktop notifications** — see the current track in your notification area
- **No external player needed** — vibez is fully self-contained, no Cider, no VLC

### ⌨️ Terminal UI

- **Fully keyboard-driven** — every action reachable without touching the mouse
- **Animated bear mascot** 🐻 — sleeps when idle, dances when music is playing
- **Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)** — a modern, composable TUI framework

### 🔌 Extensibility

- **Provider architecture** — the player core is decoupled from the music source
- **More services coming** — Spotify, YouTube Music, Deezer, and Tidal are on the roadmap

---

## Installation

### One-liner (recommended)

```sh
curl --proto '=https' --tlsv1.2 -sSf \
  https://raw.githubusercontent.com/simonepelosi/vibez/main/scripts/install.sh | sh
```

Installs the latest release binary to `~/.local/bin/` and updates your shell profile if needed.  
You can also inspect the script before running it — that's always a good idea.

> **Custom install dir:** `VIBEZ_INSTALL_DIR=/usr/local/bin curl ... | sh`

### Flatpak

Download `vibez.flatpak` from the [Releases](https://github.com/simonepelosi/vibez/releases) page:

```bash
flatpak install --user vibez.flatpak
flatpak run io.github.simonepelosi.vibez
```

> Bundles all native dependencies (WebKitGTK, GStreamer). Chrome (~150 MB) is downloaded automatically on first run.

### From source

```bash
git clone https://github.com/simonepelosi/vibez
cd vibez
make build-with-token   # requires APPLE_KEY_ID, APPLE_TEAM_ID, APPLE_PRIVATE_KEY
```

**Requirements:** Linux x86-64 · Go 1.25+ · Apple Developer Account with a MusicKit key

---

## Usage

```bash
vibez                   # launch the TUI
vibez --demo            # try vibez with built-in fake tracks — no account needed
vibez auth login        # open Apple ID login (Chrome window)
vibez auth status       # check current auth state
vibez auth logout       # clear saved tokens
vibez version           # print version
```

---

## Key Bindings

### Global

| Key | Action |
|-----|--------|
| `space` | Play / Pause |
| `n` | Next track |
| `p` | Previous track |
| `+` / `=` | Volume up |
| `-` | Volume down |
| `r` | Cycle repeat (off → all → one) |
| `s` | Toggle shuffle |
| `/` | Open search |
| `l` | Toggle library panel |
| `q` | Toggle queue panel |
| `:q` / `ctrl+c` | Quit |

### Search (`/`)

| Key | Action |
|-----|--------|
| *(type)* | Filter results in real time |
| `↑` / `↓` | Navigate results |
| `enter` | Play now |
| `tab` | Add to queue |
| `esc` | Close |

### Library (`l`)

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate list |
| `enter` | Open / play |
| `tab` | Switch tab (Playlists / Albums / Tracks) |
| `esc` | Back / close |

---

## Audio Engines

| Engine | Tracks | How it works |
|--------|--------|--------------|
| **Chrome + Widevine** *(primary)* | Full tracks | Headless Chrome via Playwright; MusicKit JS + Widevine DRM |
| **WebKit + GStreamer** *(fallback)* | 30 s previews | Embedded webkit2gtk-4.0; GStreamer decodes preview URLs |

Chrome is downloaded once to `~/.cache/vibez/playwright` and reused on every start.

---

## Architecture

```
vibez/
├── cmd/                    # CLI entry points (cobra)
├── internal/
│   ├── config/             # Config file management
│   ├── auth/               # MusicKit OAuth flow
│   ├── provider/           # Provider interface + Apple Music implementation
│   ├── player/
│   │   ├── cdp/            # Chrome CDP player (Widevine, full tracks)
│   │   ├── webkit/         # WebKit player (30 s previews)
│   │   ├── gst/            # GStreamer decoder
│   │   └── mpris/          # MPRIS D-Bus server
│   ├── tui/
│   │   ├── model.go        # Bubble Tea model + key handling
│   │   ├── views/          # Search, queue, library, now-playing, bear
│   │   └── styles/         # Lipgloss colour palette
│   └── vibe/               # Vibe agent: mood → search query
└── scripts/
    └── gen-devtoken/       # Apple MusicKit JWT generator
```

---

## Roadmap

- [x] Queue management (add, navigate, auto-advance)
- [ ] **Spotify** provider
- [ ] **YouTube Music** provider
- [ ] LLM-powered vibe agent (OpenAI / Ollama)
- [ ] Lyrics display
- [ ] Last.fm scrobbling
- [ ] Desktop notifications on track change

---

## Contributing

Open an issue before sending a PR — happy to discuss ideas.

```bash
git clone https://github.com/simonepelosi/vibez
cd vibez
go mod tidy && go build ./... && go test ./...
```

---

## License

MIT © Simone Pelosi
