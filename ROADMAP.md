# Roadmap

This document tracks planned features and integrations for vibez.  
Items are roughly ordered by expected implementation complexity.

---

## In progress

- **MPRIS icon** — resolve the GIO / GTK icon-theme lookup for the MPRIS notification
- **Demo mode** — `--demo` flag for credential-free UI development

---

## Planned

### Local tracks

Stream audio files from your local filesystem without any external service account.

- M4A, FLAC, MP3, OGG support via GStreamer or the system audio stack
- Auto-scan a configured music directory and surface tracks in the Library panel
- Works offline; no DRM or authentication required

### Spotify

Full-track streaming via Spotify's API.

- OAuth 2.0 PKCE flow (no client secret needed)
- Search, library, playlists via Spotify Web API
- Playback through Spotify Connect or librespot (open-source client)
- Requires a Spotify Premium subscription

### Deezer

Full-track streaming via Deezer's API.

- OAuth 2.0 authorisation code flow
- Search, library, playlists via Deezer REST API
- Requires a Deezer Premium subscription

### Tidal

Full-track streaming via Tidal's API.

- OAuth 2.0 authorisation code flow
- HiFi / MQA quality tiers where the subscription supports them
- Search, library, playlists, mixes via Tidal API v2

---

## Ideas / not yet scoped

- **Last.fm scrobbling** — submit plays to Last.fm in the background
- **mpd protocol bridge** — let `ncmpcpp` and other mpd clients control vibez
- **Lyrics panel** — fetch and display synced lyrics in a side pane
- **Keyboard macro recorder** — record and replay complex key sequences

---

If you'd like to work on any of these, please open an issue first so we can coordinate.
