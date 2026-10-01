package tui

import (
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
)

func TestWaitForStateStopsOnClosedStream(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		name := "empty"
		if buffered {
			name = "buffered"
		}
		t.Run(name, func(t *testing.T) {
			ch := make(chan player.State, 2)
			if buffered {
				ch <- player.State{Volume: 0.1}
				ch <- player.State{Volume: 0.5}
			}
			close(ch)
			result := make(chan any, 1)
			go func() { result <- waitForState(ch)() }()
			select {
			case msg := <-result:
				if buffered {
					state, ok := msg.(playerStateMsg)
					if !ok || state.Volume != 0.5 {
						t.Fatalf("last state = %#v, want volume 0.5", msg)
					}
				} else if msg != nil {
					t.Fatalf("closed stream returned %#v, want nil", msg)
				}
			case <-time.After(time.Second):
				t.Fatal("closed player stream kept the TUI command running")
			}
		})
	}
}
