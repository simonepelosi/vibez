//go:build linux && cgo

package gst

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func buildTestADTSStream() []byte {
	frame := []byte{
		0xff, 0xf1, 0x50, 0x80, 0x01, 0xbf, 0xfc,
		0x21, 0x10, 0x04, 0x60, 0x8c, 0x1c,
	}
	return bytes.Repeat(frame, 35)
}

func TestEmbeddedGoodPluginsFallback(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")
	if err := EnsureAACDecoder(); err != nil {
		t.Fatalf("EnsureAACDecoder: %v", err)
	}

	// Demote system gst-plugins-good elements (aacparse, souphttpsrc, curlhttpsrc)
	// so playbin3/decodebin3 must use the embedded vibezaacparse and vibezhttpsrc
	// elements and the built-in 10-band biquad equalizer.
	setElementRank("aacparse", 0)
	setElementRank("souphttpsrc", 0)
	setElementRank("curlhttpsrc", 0)
	setElementRank("equalizer-10bands", 0)
	setElementRank("autoaudiosink", 0)
	t.Cleanup(func() {
		setElementRank("aacparse", 257)
		setElementRank("souphttpsrc", 256)
		setElementRank("curlhttpsrc", 128)
		setElementRank("equalizer-10bands", 0)
		setElementRank("autoaudiosink", 256)
	})

	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Destroy()

	adts := buildTestADTSStream()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/aac")
		_, _ = w.Write(adts)
	}))
	defer srv.Close()

	startedCh := make(chan string, 1)
	eosCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)

	p.OnStreamStart(func(u string) {
		select {
		case startedCh <- u:
		default:
		}
	})
	p.OnEOS(func() {
		select {
		case eosCh <- struct{}{}:
		default:
		}
	})
	p.OnError(func(e error) {
		select {
		case errCh <- e:
		default:
		}
	})

	p.SetVolume(0.8)
	p.SetEqualizer([]float64{6, 4, 2, 0, -2, -2, 0, 2, 4, 6})

	u1 := srv.URL + "/track1.aac"
	u2 := srv.URL + "/track2.aac"
	p.PlayURI(u1)
	p.SetNextURI(u2)

	select {
	case got := <-startedCh:
		if got != u2 {
			t.Fatalf("OnStreamStart URI = %q, want %q", got, u2)
		}
	case e := <-errCh:
		t.Fatalf("pipeline error before gapless transition: %v", e)
	case <-eosCh:
		t.Fatal("received EOS before gapless OnStreamStart")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for gapless OnStreamStart")
	}

	select {
	case <-eosCh:
	case e := <-errCh:
		t.Fatalf("pipeline error during second track: %v", e)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for EOS after second track")
	}
}
