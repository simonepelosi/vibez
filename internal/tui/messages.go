package tui

import (
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// InitStatusMsg updates the status text shown on the loading screen.
type InitStatusMsg string

// EngineReadyMsg is sent when the audio engine and provider are both ready.
type EngineReadyMsg struct {
	Player   player.Player
	Provider provider.Provider
}

// InitErrMsg is sent when initialization fails fatally.
type InitErrMsg struct{ Err error }
