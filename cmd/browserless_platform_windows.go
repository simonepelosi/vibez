//go:build windows

package cmd

import (
	"fmt"

	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/player/smtc"
)

func browserlessBackendLabel(audioBitrateKbps int) string {
	return fmt.Sprintf("Browserless (In-Process Widevine + Media Foundation) · %d kbps AAC", audioBitrateKbps)
}

// setupBrowserlessPlatform publishes playback to the System Media Transport
// Controls, which the media keys, the volume flyout and widgets such as YASB
// use. SMTC is optional: when it cannot start, vibez logs why and plays on.
func setupBrowserlessPlatform(p *browserless.Player, log func(string)) {
	srv, err := smtc.NewServer(p, log)
	if err != nil {
		log("[smtc] media keys and the now-playing widget are unavailable: " + err.Error())
		return
	}
	updates := p.Subscribe()
	go func() {
		defer func() { _ = srv.Close() }()
		for st := range updates {
			srv.Update(st)
		}
	}()
}
