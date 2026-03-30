# ♪ vibez

> **vibe-driven music player for your terminal**

vibez is an open-source TUI (terminal user interface) music player and controller for Linux. It connects to your Apple Music library, lets you search, browse, and control playback — all from the terminal — and is designed to be extended to other providers (Spotify, YouTube Music, Deezer) in the future.

Playback is handled by any MPRIS-compatible player (e.g. [Cider](https://cider.sh/)), so vibez acts as a smart remote and library browser, not a standalone audio engine.

---

## Features

- 🎵 Browse your Apple Music library (playlists, albums, tracks)
- 🔍 Search the Apple Music catalog in real-time
- 🎮 Control playback via MPRIS D-Bus (play, pause, next, previous, volume, seek)
- 🧠 Vibe agent: type a mood ("coding", "chill", "gym") and get a matching search query
- ⌨️  Fully keyboard-driven TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- 🔌 Extensible provider architecture (Spotify, YouTube Music coming soon)

---

## Requirements

- **Linux** with a D-Bus session bus
- **Go 1.26+**
- **Apple Developer Account** with a MusicKit key (for Apple Music)
- **An MPRIS-compatible player** installed and running for audio playback
  - [Cider](https://cider.sh/) (recommended — full Apple Music experience on Linux)
  - Any other player that exposes `org.mpris.MediaPlayer2`

---

## Installation

```bash
go install github.com/simone-vibes/vibez@latest
```

Or clone and build locally:

```bash
git clone https://github.com/simone-vibes/vibez
cd vibez
go build -o vibez .
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
5. Generate a signed JWT:

```bash
# Using jwt-cli (https://github.com/mike-engel/jwt-cli) or any JWT tool
# Header: { "alg": "ES256", "kid": "<KEY_ID>" }
# Payload: { "iss": "<TEAM_ID>", "iat": <now>, "exp": <now+15776000> }
```

6. Paste the JWT into `apple_developer_token` in `~/.config/vibez/config.json`
7. Fill in `apple_key_id` and `apple_team_id` as well

---

## Usage

### Log in to Apple Music

```bash
vibez auth login
```

This starts a local web server, opens your browser, and uses MusicKit JS to authorize your Apple Music account. The user token is saved automatically.

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

| Key | Action |
|-----|--------|
| `1` | Now Playing view |
| `2` | Queue view |
| `3` | Library view |
| `4` / `/` | Search view |
| `space` | Play / Pause |
| `n` | Next track |
| `p` | Previous track |
| `+` / `=` | Volume up |
| `-` | Volume down |
| `↑` / `k` | Navigate up |
| `↓` / `j` | Navigate down |
| `enter` | Select item |
| `tab` | Switch library tab (Playlists / Albums / Tracks) |
| `?` | Toggle help |
| `q` / `ctrl+c` | Quit |

---

## Architecture

```
vibez/
├── cmd/               # CLI commands (cobra)
├── internal/
│   ├── config/        # Config file management
│   ├── auth/          # MusicKit JS auth flow
│   ├── provider/      # Provider interface + Apple Music implementation
│   ├── player/        # Player interface + MPRIS D-Bus implementation
│   ├── tui/           # Bubble Tea TUI (model, views, styles, keys)
│   └── vibe/          # Vibe agent: mood/keyword → search query
└── web/               # Embedded HTML for auth login page
```

The **provider** interface makes it easy to add new music services:

```go
type Provider interface {
    Name() string
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
    // ...
}
```

---

## Roadmap

- [ ] **Spotify** provider (OAuth2 + Web API)
- [ ] **YouTube Music** provider
- [ ] **Deezer** provider
- [ ] LLM-powered vibe agent (OpenAI / Ollama)
- [ ] Queue management (add, remove, reorder)
- [ ] Lyrics display
- [ ] Last.fm scrobbling
- [ ] Notification support

---

## Contributing

Contributions are welcome! Please open an issue to discuss your idea before sending a PR.

```bash
git clone https://github.com/simone-vibes/vibez
cd vibez
go mod tidy
go build ./...
go test ./...
```

---

## License

MIT © Simone Pelosi
