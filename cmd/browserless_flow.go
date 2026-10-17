//go:build linux

package cmd

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/auth"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/player/mpris"
	"github.com/simone-vibes/vibez/internal/provider/apple"
	"github.com/simone-vibes/vibez/internal/tui"
	"github.com/simone-vibes/vibez/internal/updater"
	"github.com/simone-vibes/vibez/internal/version"
)

func runBrowserlessFlow(cfg *config.Config, opts tui.Options, onUserToken, onStorefront func(string), audioBitrateKbps int) error {
	prog := tea.NewProgram(tui.New(cfg, nil, nil, opts))
	playerCh := make(chan *browserless.Player, 1)
	restartExe := make(chan string, 1)
	var stopRPC func()

	go func() {
		if exe := updater.CheckAndUpdate(version.Version, noUpdate, func(msg string) {
			prog.Send(tui.InitStatusMsg(msg))
		}); exe != "" {
			restartExe <- exe
			prog.Send(tui.RestartMsg{})
			return
		}

		devTokenRejected := false
		if cfg.AppleUserToken != "" {
			prog.Send(tui.InitStatusMsg("Checking Apple Music session..."))
			switch status, sf := auth.CheckTokens(cfg.AppleDeveloperToken, cfg.AppleUserToken); status {
			case auth.UserTokenRejected:
				prog.Send(tui.InitStatusMsg("Session expired - re-authenticating..."))
				cfg.AppleUserToken = ""
				_ = cfg.Save("")
			case auth.DeveloperTokenRejected:
				devTokenRejected = true
				res := updater.UpdateNow(version.Version, noUpdate, func(msg string) {
					prog.Send(tui.InitStatusMsg(msg))
				})
				if res.Outcome == updater.OutcomeInstalled {
					restartExe <- res.Exe
					prog.Send(tui.RestartMsg{})
					return
				}
				statusMsg := auth.DeveloperTokenStatus
				if advice := res.Advice(); advice != "" {
					statusMsg = auth.DeveloperTokenPrefix + " - " + advice
				}
				prog.Send(tui.InitStatusMsg(statusMsg))
			case auth.TokensValid:
				if onStorefront != nil {
					onStorefront(sf)
				}
			}
		}

		if cfg.AppleUserToken == "" {
			prog.Send(tui.InitStatusMsg("Authorizing with Apple Music..."))
			if err := auth.Login(cfg); err != nil {
				prog.Send(tui.InitErrMsg{Err: fmt.Errorf("authentication: %w", err)})
				return
			}
			if onUserToken != nil {
				onUserToken(cfg.AppleUserToken)
			}
			if _, sf := auth.CheckTokens(cfg.AppleDeveloperToken, cfg.AppleUserToken); sf != "" && onStorefront != nil {
				onStorefront(sf)
			}
		}

		if !devTokenRejected {
			prog.Send(tui.InitStatusMsg("Starting browserless audio engine..."))
		}
		prov := apple.New(cfg)
		blPlayer, err := browserless.New(cfg, prov)
		if err != nil {
			prog.Send(tui.InitErrMsg{Err: fmt.Errorf("audio engine: %w", err)})
			return
		}
		_ = blPlayer.SetAudioBitrate(audioBitrateKbps)

		playerCh <- blPlayer

		// MPRIS Linux media key & status bar integration
		if srv, mprisErr := mpris.NewServer(blPlayer); mprisErr == nil {
			go func() {
				for st := range blPlayer.Subscribe() {
					srv.Update(st)
				}
			}()
		}

		// Last.fm scrobbler & Discord RPC integration
		startLastfmScrobbler(cfg, blPlayer, func(msg string) { prog.Send(tui.DebugLogMsg(msg)) })
		stopRPC = startDiscordRPC(cfg, blPlayer, func(msg string) { prog.Send(tui.DebugLogMsg(msg)) })

		backendLabel := fmt.Sprintf("Browserless (In-Process Widevine + GStreamer) · %d kbps AAC", audioBitrateKbps)
		prog.Send(tui.EngineReadyMsg{
			Player:   blPlayer,
			Provider: prov,
			Backend:  backendLabel,
		})
	}()

	_, err := prog.Run()
	if stopRPC != nil {
		stopRPC()
	}

	select {
	case p := <-playerCh:
		_ = p.Close()
	default:
	}

	select {
	case exe := <-restartExe:
		if restartErr := restartProcess(exe); restartErr != nil {
			return errors.Join(err, fmt.Errorf("restarting into the update at %s: %w", exe, restartErr))
		}
	default:
	}

	return err
}
