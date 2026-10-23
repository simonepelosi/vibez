//go:build windows && cgo

package browserless

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestWindowsSinkKeepsPositionAtEOS plays through a real waveOut device, not
// the VIBEZ_NULL_AUDIO frame counter, because only the device path closed the
// output before announcing the end of the stream. Position() then read 0 inside
// the OnEOS callback, the player took every natural track end for a premature
// one, and the queue never advanced. It skips when there is no output device,
// as on CI runners.
func TestWindowsSinkKeepsPositionAtEOS(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "")

	adts := buildTestADTSStream()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/aac")
		_, _ = w.Write(adts)
	}))
	defer srv.Close()

	s, err := newAudioSink()
	if err != nil {
		t.Fatalf("newAudioSink: %v", err)
	}
	defer s.Destroy()

	posAtEOS := make(chan time.Duration, 1)
	errCh := make(chan error, 1)
	// The player reads the position inside its OnEOS callback, so read it here too.
	s.OnEOS(func() {
		select {
		case posAtEOS <- s.Position():
		default:
		}
	})
	s.OnError(func(e error) {
		select {
		case errCh <- e:
		default:
		}
	})

	s.SetVolume(0)
	s.PlayURI(srv.URL + "/stream.aac")

	select {
	case pos := <-posAtEOS:
		// 35 AAC frames of 1024 samples at 44.1 kHz play for about 0.81 s.
		if pos < 500*time.Millisecond {
			t.Fatalf("Position at EOS = %v, want about 0.8s of played audio", pos)
		}
		t.Logf("Position at EOS: %v", pos)
	case e := <-errCh:
		if strings.Contains(e.Error(), "cannot open audio output device") {
			t.Skipf("no audio output device: %v", e)
		}
		t.Fatalf("native sink reported error: %v", e)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for EOS")
	}
}
