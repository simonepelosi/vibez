//go:build windows && cgo

package browserless

import (
	"testing"
	"time"
)

// A runner has no sound card, so playback itself cannot be exercised here, but
// the native plumbing can: the worker thread, Media Foundation loading, the
// error callback into Go and teardown.
func TestWindowsSinkReportsUnreachableStream(t *testing.T) {
	s, err := newAudioSink()
	if err != nil {
		t.Fatalf("newAudioSink: %v", err)
	}
	defer s.Destroy()

	errCh := make(chan error, 1)
	s.OnError(func(e error) {
		select {
		case errCh <- e:
		default:
		}
	})

	s.PlayURI("http://127.0.0.1:1/stream/none/none.aac")

	select {
	case e := <-errCh:
		t.Logf("got expected error: %v", e)
	case <-time.After(30 * time.Second):
		t.Fatal("no error reported for an unreachable stream")
	}
}

func TestWindowsSinkControlsAreSafeWithoutAStream(t *testing.T) {
	s, err := newAudioSink()
	if err != nil {
		t.Fatalf("newAudioSink: %v", err)
	}

	s.Play()
	s.Pause()
	s.Seek(5 * time.Second)
	s.SetVolume(0.5)
	s.SetEqualizer([]float64{3, 2, 1, 0, 0, 0, -1, -2, -3, -4})
	if pos := s.Position(); pos != 0 {
		t.Errorf("Position with no stream = %v, want 0", pos)
	}
	s.Stop()
	s.Destroy()
	s.Destroy() // must be idempotent
}
