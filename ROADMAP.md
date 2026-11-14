# Roadmap

This document tracks implemented milestones and planned features for **vibez**.

---

## Shipped Highlights

- **In-process Widevine CDM & native audio sinks** — zero Chrome/Playwright/Node.js dependencies across Linux (GStreamer), macOS (Core Audio), and Windows (Media Foundation + `waveOut`)
- **Offline Local Mode (`--local`) & Hybrid Lossless Mode (`--hybrid`)** — recursive library indexing, format support across platforms, and automatic ISRC/metadata lossless substitution inside Apple Music queues
- **10-band Parametric Equalizer (`e`)** — real-time native biquad / GStreamer filtering persisted to config
- **Synced Lyrics (`y`)** — LRCLIB integration with line & word highlighting and lyric-to-timestamp seeking
- **AI-powered Vibe Mode (`v`)** — automatic detection of local LLM runtimes (Ollama, LM Studio, `llama.cpp`) with transparent fallback to the built-in keyword mood planner
- **Discovery (`d`) & Radio (`R`) Modes** — adjustable similarity engine and track-seeded endless stations
- **System Integration** — Linux MPRIS D-Bus + desktop icon/entry, Windows SMTC, Discord Rich Presence, Last.fm scrobbling, `--demo` mode, custom JSON themes, and verified SHA-256 self-updater

---

## Planned

### Spotify provider

Full-track streaming and library browsing via Spotify:

- OAuth 2.0 PKCE authentication flow (no client secret needed)
- Search, library, and playlist browsing via Spotify Web API
- Playback via Spotify Connect or `librespot` integration (Spotify Premium required)

### YouTube Music provider

Catalog search, playlists, and native audio streaming for YouTube Music.

### Deezer provider

Full-track streaming via Deezer's API (OAuth 2.0 flow + search/library/playlists).

### Tidal provider

Catalog search, playlists, and lossless playback tiers via Tidal API.

### Standalone desktop track-change notifications

Native OS toast notifications (`org.freedesktop.Notifications` / `NSUserNotification` / Windows Toast) on track transitions, independent of MPRIS/SMTC media widgets.

---

## Ideas / Future Exploration

- **`mpd` protocol bridge** — allow `ncmpcpp` and other `mpd` clients to control vibez playback and queues
- **Keyboard macro recorder** — record and replay custom key sequences

---

If you'd like to help build any of these, open an issue first so we can align on the design.
