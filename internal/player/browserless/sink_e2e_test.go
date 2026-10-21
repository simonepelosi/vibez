//go:build (linux || darwin || windows) && cgo

package browserless

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

// buildTestADTSStream returns a ~0.8s valid ADTS AAC-LC 44.1kHz stereo stream
// (~35 frames of 1024 samples each).
func buildTestADTSStream() []byte {
	frame := []byte{
		0xff, 0xf1, 0x50, 0x80, 0x01, 0xbf, 0xfc,
		0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c,
	}
	return bytes.Repeat(frame, 35)
}

// TestNativeSinkPlaysADTSStreamEndToEnd exercises the platform's native audio
// sink (GStreamer playbin3 on Linux, Core Audio AudioFileStream + AudioConverter
// + AudioQueue on macOS, Media Foundation IMFSourceReader + waveOut on Windows)
// against a real loopback HTTP ADTS AAC stream, including the 10-band equalizer,
// volume control, position reporting, and end-of-stream (OnEOS) delivery.
func TestNativeSinkPlaysADTSStreamEndToEnd(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")

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

	eosCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)
	s.OnEOS(func() {
		select {
		case eosCh <- struct{}{}:
		default:
		}
	})
	s.OnError(func(e error) {
		select {
		case errCh <- e:
		default:
		}
	})

	s.SetVolume(0.75)
	s.SetEqualizer([]float64{3, 2, 1, 0, -1, -2, 0, 1, 2, 3})
	s.PlayURI(srv.URL + "/stream.aac")

	var maxPos time.Duration
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-ticker.C:
			if pos := s.Position(); pos > maxPos {
				maxPos = pos
			}
		case e := <-errCh:
			t.Fatalf("native sink reported error: %v", e)
		case <-eosCh:
			if pos := s.Position(); pos > maxPos {
				maxPos = pos
			}
			if maxPos <= 0 {
				t.Errorf("expected observed playback position > 0 before EOS, got %v", maxPos)
			}
			t.Logf("native sink reached EOS cleanly (max observed position: %v)", maxPos)
			return
		case <-timeout.C:
			t.Fatalf("timed out waiting for native sink EOS (max observed position: %v)", maxPos)
		}
	}
}

// TestNativeSinkGaplessTransitionEndToEnd verifies that on Linux (GStreamer
// playbin3/playbin) pre-staging a second ADTS stream via SetNextURI transitions
// gaplessly on about-to-finish, fires OnStreamStart with the exact committed
// URI, and then reaches OnEOS at the end of the second stream.
func TestNativeSinkGaplessTransitionEndToEnd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SetNextURI / OnStreamStart gapless sink handoff is specific to GStreamer")
	}
	t.Setenv("VIBEZ_NULL_AUDIO", "1")

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

	stream1 := srv.URL + "/track1.aac"
	stream2 := srv.URL + "/track2.aac"

	startedCh := make(chan string, 1)
	eosCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)

	s.OnStreamStart(func(uri string) {
		select {
		case startedCh <- uri:
		default:
		}
	})
	s.OnEOS(func() {
		select {
		case eosCh <- struct{}{}:
		default:
		}
	})
	s.OnError(func(e error) {
		select {
		case errCh <- e:
		default:
		}
	})

	s.PlayURI(stream1)
	s.SetNextURI(stream2)

	select {
	case gotURI := <-startedCh:
		if gotURI != stream2 {
			t.Fatalf("OnStreamStart URI = %q, want %q", gotURI, stream2)
		}
	case e := <-errCh:
		t.Fatalf("sink error before gapless transition: %v", e)
	case <-eosCh:
		t.Fatal("received EOS before gapless OnStreamStart")
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for gapless OnStreamStart")
	}

	select {
	case <-eosCh:
	case e := <-errCh:
		t.Fatalf("sink error during second track: %v", e)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for EOS after second track")
	}
}
