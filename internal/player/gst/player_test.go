//go:build linux && cgo

package gst

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// tinyM4AHex is 0.3 s of 8 kHz mono AAC-LC silence in an MP4 container, from:
//
//	ffmpeg -f lavfi -i anullsrc=r=8000:cl=mono -t 0.3 -c:a aac -b:a 8k \
//	    -map_metadata -1 -fflags +bitexact -flags +bitexact tiny.m4a
const tinyM4AHex = `
0000001c667479704d344120000002004d34412069736f6d69736f32000002d66d6f6f760000006c6d76686400000000
000000000000000000001f40000009600001000001000000000000000000000000010000000000000000000000000000
000100000000000000000000000000004000000000000000000000000000000000000000000000000000000000000002
000002257472616b0000005c746b68640000000300000000000000000000000100000000000009600000000000000000
000000010100000000010000000000000000000000000000000100000000000000000000000000004000000000000000
0000000000000024656474730000001c656c737400000000000000010000096000000400000100000000019d6d646961
000000206d64686400000000000000000000000000001f4000000d6055c400000000002d68646c720000000000000000
736f756e000000000000000000000000536f756e6448616e646c657200000001486d696e6600000010736d6864000000
00000000000000002464696e660000001c6472656600000000000000010000000c75726c20000000010000010c737462
6c0000006a7374736400000000000000010000005a6d7034610000000000000001000000000000000000010010000000
001f40000000000036657364730000000003808080250001000480808017401500000000001f400000012b0580808005
158856e50006808080010200000020737474730000000000000002000000030000040000000001000001600000001c73
7473630000000000000001000000010000000400000001000000147374737a0000000000000004000000040000001473
74636f0000000000000001000003020000001a7367706401000000726f6c6c0000000200000001ffff0000001c736267
7000000000726f6c6c0000000100000004000000010000003d75647461000000356d657461000000000000002168646c
7200000000000000006d6469726170706c00000000000000000000000008696c73740000000866726565000000186d64
617401182007011820070118200701182007
`

// Local files reach the parser as raw framed AAC from qtdemux, with no ADTS
// headers. vibezaacparse outranks aacparse, so its sink caps must leave raw
// streams alone; otherwise decodebin3 plugs it after qtdemux, it scans the raw
// frames for ADTS syncwords, drops every buffer, and --local playback never
// leaves 0:00.
func TestLocalM4APlaysToEOS(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")
	if err := EnsureAACDecoder(); err != nil {
		t.Fatalf("EnsureAACDecoder: %v", err)
	}

	m4a, err := hex.DecodeString(strings.Join(strings.Fields(tinyM4AHex), ""))
	if err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "tiny.m4a")
	if err := os.WriteFile(path, m4a, 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	p, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Destroy()

	eosCh := make(chan struct{}, 1)
	errCh := make(chan error, 1)
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

	p.PlayURI("file://" + path)

	select {
	case <-eosCh:
	case e := <-errCh:
		t.Fatalf("pipeline error playing local .m4a: %v", e)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for EOS: local .m4a never played")
	}
}
