package tui

import (
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Options configures optional TUI features at startup.
type Options struct {
	// MemProfiling enables live RSS display (vibez + helper) in the header.
	// Activate with --mem-profiling on the command line.
	MemProfiling bool
	// IconPath is the absolute path of the installed app icon (may be empty).
	IconPath string
	// Backend is a human-readable description of the audio backend, logged in
	// the debug view on startup. Leave empty if the engine starts asynchronously
	// (CDP path uses EngineReadyMsg.Backend instead).
	Backend string
}

// InitStatusMsg updates the status text shown on the loading screen.
type InitStatusMsg string

// EngineReadyMsg is sent when the audio engine and provider are both ready.
type EngineReadyMsg struct {
	Player      player.Player
	Provider    provider.Provider
	HelperPaths []string // absolute paths of the Chrome helper binary (for RSS tracking)
	Backend     string   // human-readable backend description, shown in the debug log
}

// InitErrMsg is sent when initialization fails fatally.
type InitErrMsg struct{ Err error }

// DebugLogMsg appends a line to the TUI debug log (visible via :debug-logs).
// Use this to surface background-goroutine events (e.g. scrobbling) without
// printing to stderr.
type DebugLogMsg string
