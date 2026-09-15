//go:build linux

package cmd

import (
	"fmt"

	"github.com/simone-vibes/vibez/internal/player/browserless"
	"github.com/simone-vibes/vibez/internal/player/mpris"
)

func browserlessBackendLabel(audioBitrateKbps int) string {
	return fmt.Sprintf("Browserless (In-Process Widevine + GStreamer) · %d kbps AAC", audioBitrateKbps)
}

func setupBrowserlessPlatform(p *browserless.Player) {
	if srv, mprisErr := mpris.NewServer(p); mprisErr == nil {
		go func() {
			for st := range p.Subscribe() {
				srv.Update(st)
			}
		}()
	}
}
