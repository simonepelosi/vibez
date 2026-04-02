<p align="center">
  <img src="assets/logo.png" width="160" alt="vibez logo">
</p>

<h1 align="center">vibez</h1>

<p align="center">
  <strong>Apple Music in your terminal. Vibe-driven. Keyboard-first.</strong>
</p>

<p align="center">
  <a href="https://github.com/simonepelosi/vibez/actions"><img src="https://img.shields.io/github/actions/workflow/status/simonepelosi/vibez/release.yml?style=flat-square&label=CI" alt="CI"></a>
  <a href="https://github.com/simonepelosi/vibez/releases"><img src="https://img.shields.io/github/v/release/simonepelosi/vibez?style=flat-square" alt="Release"></a>
  <a href="https://github.com/simonepelosi/vibez/blob/main/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/simonepelosi/vibez?style=flat-square" alt="Go version"></a>
  <a href="https://github.com/simonepelosi/vibez/blob/main/LICENSE"><img src="https://img.shields.io/github/license/simonepelosi/vibez?style=flat-square" alt="License"></a>
</p>

<p align="center">
  <a href="#installation">Installation</a> · <a href="#usage">Usage</a> · <a href="#configuration">Configuration</a> · <a href="#key-bindings">Key Bindings</a> · <a href="#roadmap">Roadmap</a>
</p>

---

vibez is an open-source TUI Apple Music player for Linux. Search, queue, and control playback entirely from the keyboard — no Cider, no VLC, no external app.

Full tracks stream via an embedded headless Chrome with Widevine DRM (auto-downloaded). Falls back to WebKit + GStreamer (30 s previews) when Chrome is unavailable. MPRIS support means desktop media keys and notifications just work.

---

## Features

- 🎵 Browse your Apple Music library — playlists, albums, tracks
- 🔍 Real-time search of the Apple Music catalog
- 🎶 Full-track streaming via Chrome + Widevine DRM
- 📋 Queue management — add with `tab`, skip with `n`/`p`
- 🐻 Animated bear mascot that sleeps when idle and dances when music plays
- 🖥️ MPRIS D-Bus — desktop media keys and notifications out of the box
- ⌨️ Fully keyboard-driven TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- 🔌 Extensible provider architecture (Spotify, YouTube Music planned)

---

## Installation

### Flatpak (recommended)

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

## Configuration

vibez stores config at `~/.config/vibez/config.json` (auto-created on first run):

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

1. Go to [developer.apple.com](https://developer.apple.com/account/resources/authkeys/list) → create a key with **MusicKit** capability
2. Download the `.p8` file, note your **Key ID** and **Team ID**
3. Generate a signed JWT with the bundled helper:

```bash
go run ./scripts/gen-devtoken \
  --key-id   <KEY_ID>   \
  --team-id  <TEAM_ID>  \
  --key-file <path/to/AuthKey_XXXXXX.p8>
```

4. Paste the output into `apple_developer_token` in `config.json`

---

## Usage

```bash
vibez auth login    # open Apple ID login (Chrome window)
vibez auth status   # check current auth state
vibez auth logout   # clear saved tokens
vibez               # launch the TUI
vibez version       # print version
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
