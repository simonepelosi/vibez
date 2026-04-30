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

vibez is an open-source TUI Apple Music player for Linux. Search, queue, and control playback entirely from the keyboard.

Full tracks stream via an embedded headless Chrome with Widevine DRM (auto-downloaded). Falls back to WebKit + GStreamer (30 s previews) when Chrome is unavailable. MPRIS support means desktop media keys and notifications just work.

---

## Features

### 🎵 Music Playback

- **Full-track streaming** via headless Chrome + Widevine DRM — the real deal, not 30-second clips
- **Automatic fallback** to WebKit + GStreamer (30 s previews) when Chrome is unavailable
- **Playback controls** — play/pause, next, previous, seek ±10 s, volume up/down
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
- **Last.fm scrobbling** — optional integration; connect with `vibez auth lastfm login` and your listening history is tracked automatically

### 🌀 Vibe Mode

- **Describe music in plain English** — press `v`, type your mood or activity ("late night coding", "Sunday morning chill"), and vibez builds a queue of matching tracks
- **Keyword-based mood engine** — maps your description to a mood, energy level, genres, and multiple search query variants for variety
- **Diverse results** — runs several searches and shuffles up to 15 tracks into your queue so it never feels repetitive
- **Works for any occasion** — focus, workout, party, road trip, heartbreak, romance, and more

### 🔭 Discovery Mode

- **Continuous automatic queuing** — press `d` to turn on discovery mode; vibez finds a similar track as soon as the last song in the queue starts playing, so the music never stops. Trigger timing will be fully configurable in a future release
- **Adjustable similarity** — use `+`/`-` to dial between "same artist" (0.9) and "pure discovery" (0.0), giving you full control over how adventurous the next pick is
- **Seed-aware** — the currently playing track is used as the seed; searches adapt progressively from same artist → same genre → completely random as similarity decreases
- **Toggle anytime** — press `d` again to stop discovery and return to a manual queue

### ⌨️ Terminal UI

- **Fully keyboard-driven** — every action reachable without touching the mouse
- **Vim-style command mode** — press `:` to run commands like `:save <name>`, `:vol 80`, `:mute`, or `:q` / `:quit` to exit
- **Vim-style navigation** — `gg` to jump to top, `G` to jump to bottom, `j`/`k` for list scrolling in panels
- **Animated bear mascot** 🐻 — sleeps when idle, dances when music is playing
- **Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)** — a modern, composable TUI framework

### 🔌 Extensibility

- **Provider architecture** — the player core is decoupled from the music source
- **More services coming** — Spotify, YouTube Music, Deezer, and Tidal are on the roadmap

### 🎨 Themes

- **Built-in themes** — `default` (Tokyo Night / Catppuccin), `dracula`, `gruvbox`, `nord`
- **Custom themes** — create your own palette as a JSON file in `~/.config/vibez/themes/<name>.json`

---

## Installation

### One-liner (recommended)

```sh
curl --proto '=https' --tlsv1.2 -sSf \
  https://raw.githubusercontent.com/simonepelosi/vibez/main/scripts/install.sh | sh
```

Installs the latest release binary to `~/.local/bin/` and updates your shell profile if needed.  
You can also inspect the script before running it — that's always a good idea.

> **Update:** re-running the same command updates vibez to the latest release.

> **Custom install dir:** `VIBEZ_INSTALL_DIR=/usr/local/bin curl ... | sh`

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
vibez                       # launch the TUI
vibez --demo                # try vibez with built-in fake tracks — no account needed
vibez auth login            # open Apple ID login (Chrome window)
vibez auth status           # check current auth state
vibez auth logout           # clear saved tokens
vibez auth lastfm login     # connect your Last.fm account (optional)
vibez auth lastfm status    # check Last.fm connection status
vibez auth lastfm logout    # disconnect Last.fm
vibez version               # print version
```

---

## Theming

Set the `theme` key in `~/.config/vibez/config.json`:

```json
{
  "theme": "dracula"
}
```

**Built-in themes:** `default`, `dracula`, `gruvbox`, `nord`

### Custom themes

Create `~/.config/vibez/themes/<name>.json` with any subset of fields — missing or invalid values fall back to `default`:

```json
{
  "primary":      "#ff79c6",
  "secondary":    "#50fa7b",
  "muted":        "#6272a4",
  "error":        "#ff5555",
  "fg":           "#f8f8f2",
  "subtle":       "#8be9fd",
  "bg":           "#282a36",
  "love":         "#ff6e6e",
  "active":       "#50fa7b",
  "progress":     "#8be9fd",
  "surface":      "#44475a",
  "accent":       "#bd93f9",
  "accent_warm":  "#f1fa8c",
  "bear":         "#ffb86c",
  "glow_palette": ["#282a36","#383a52","#44475a","#6272a4","#9580ff","#bd93f9","#caa9fa","#e9e0ff"],
  "mode_normal_bg":  "#50fa7b",
  "mode_search_bg":  "#8be9fd",
  "mode_command_bg": "#f1fa8c",
  "mode_chip_fg":    "#282a36"
}
```

Then set `"theme": "<name>"` in `config.json` and restart vibez.

---

## Key Bindings

### Global

| Key | Action |
|-----|--------|
| `space` | Play / Pause |
| `n` | Next track |
| `p` | Previous track |
| `←` / `→` | Seek backward / forward 10 s |
| `+` / `=` | Volume up |
| `-` | Volume down |
| `r` | Cycle repeat (off → all → one) |
| `s` | Toggle shuffle |
| `f` | Heart / favourite current track |
| `v` | Open vibe input (mood-driven search) |
| `d` | Toggle discovery mode |
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

### Queue (`q`)

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate list |
| `enter` | Play selected track |
| `d` | Remove track from queue |
| `K` | Move track up |
| `J` | Move track down |
| `c` | Clear entire queue |
| `s` | Save queue as playlist (opens command prompt) |
| `esc` | Close |

### Command mode (`:`)

Vim-style command mode — press `:` from anywhere to open the command prompt.

| Command | Description |
|---------|-------------|
| `:save <name>` | Save the current queue as an Apple Music playlist |
| `:discover <n>\|auto` | Queue *n* discovered songs now, or keep auto-discovering |
| `:vol <0-100>` | Set volume to an absolute level (e.g. `:vol 80`) |
| `:vol +n` / `:vol -n` | Raise or lower volume by *n* percent (e.g. `:vol +10`) |
| `:vol` | Show current volume in the status bar |
| `:mute` | Toggle mute (run again to restore the previous volume) |
| `:seek <seconds>` | Jump to an absolute position in the current song |
| `:debug-logs` | Toggle the debug log panel |
| `:q` / `:quit` | Quit vibez |

Use `↑` / `↓` (or `ctrl+p` / `ctrl+n`) to cycle through suggestions, and `tab` to autocomplete.

### Discovery mode (`d`)

| Key | Action |
|-----|--------|
| `+` / `=` | Increase similarity (stay closer to current artist / genre) |
| `-` | Decrease similarity (explore further afield) |
| `d` | Stop discovery mode |

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
│   ├── lastfm/             # Last.fm scrobbling (optional)
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
- [x] Last.fm scrobbling
- [ ] **Spotify** provider
- [ ] **YouTube Music** provider
- [ ] LLM-powered vibe agent (OpenAI / Ollama)
- [ ] Lyrics display
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
