package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Options configures optional TUI features at startup.
type Options struct {
	// MemProfiling enables live RSS display in the header.
	// Activate with --mem-profiling on the command line.
	MemProfiling bool
	// IconPath is the absolute path of the installed app icon (may be empty).
	IconPath string
	// Backend is a human-readable description of the audio backend, logged in
	// the debug view on startup. Leave empty if the engine starts asynchronously.
	Backend string
	// InitialTracks pre-populates the queue panel on launch (used by local mode).
	InitialTracks []provider.Track
	// ScanNotice summarises what a local-mode directory scan left out. It is
	// recorded in the debug log on startup, and shown in the status bar when
	// the scan produced no tracks at all, because an empty library otherwise
	// looks identical to a mistyped path.
	ScanNotice string
	// InitialLogs pre-populates the debug log on launch (e.g. broker token fetch logs).
	InitialLogs []string
	// OnReauth registers an Apple Music login attempt on the event loop and
	// returns a command for the blocking work, or nil if no attempt was started.
	OnReauth func() tea.Cmd
	// OnLogout optionally tears down any in-flight re-auth and suppresses automatic re-auth after :logout.
	OnLogout func()
	// HybridEnabled indicates whether hybrid local-playback matching is active.
	HybridEnabled bool
	// HybridDir is the configured music directory shown in hybrid diagnostics.
	HybridDir string
	// OnRescan triggers a background local-library rescan (used by :rescan).
	// Returns whether a scan started and an optional status banner message.
	OnRescan func() (started bool, msg string)
}

// InitStatusMsg updates the status text shown on the loading screen.
type InitStatusMsg string

// EngineReadyMsg is sent when the audio engine and provider are both ready.
type EngineReadyMsg struct {
	Player   player.Player
	Provider provider.Provider
	Backend  string // human-readable backend description, shown in the debug log
}

// InitErrMsg is sent when initialization fails fatally.
type InitErrMsg struct{ Err error }

// RestartMsg is sent when vibez has self-updated and needs to re-exec.
// The model handles it by quitting cleanly so the caller can exec the new binary.
type RestartMsg struct{}

// DebugLogMsg appends a line to the TUI debug log (visible via :debug-logs).
// Use this to surface background-goroutine events (e.g. scrobbling) without
// printing to stderr.
type DebugLogMsg string

// HybridStatusMsg updates the TUI's hybrid local-playback indicator in the Now Playing header.
type HybridStatusMsg struct {
	Enabled  bool
	Scanning bool
	Count    int
	Err      string
}

// playlistsForPickerMsg is the response to a library-playlists fetch for the
// playlist picker modal.
type playlistsForPickerMsg struct {
	playlists []provider.Playlist
	err       error
	gen       int // stale-response guard; discarded if != m.playlistPickerGen
	authGen   uint64
}

// trackAddedToPlaylistMsg is the response to an AddToPlaylist call.
type trackAddedToPlaylistMsg struct {
	playlistName string
	err          error
	authGen      uint64
}
