//go:build linux && cgo

package gst

import (
	"bytes"
	"encoding/binary"
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

	playToEOS(t, path)
}

// playToEOS plays a local file through the real pipeline and fails unless it
// reaches end of stream.
func playToEOS(t *testing.T, path string) time.Duration {
	t.Helper()
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

	name := filepath.Base(path)
	select {
	case <-eosCh:
	case e := <-errCh:
		t.Fatalf("pipeline error playing %s: %v", name, e)
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for EOS: %s never played", name)
	}
	return p.Duration()
}

// pcmWAV returns a silent stereo WAV of the given bit depth and rate.
func pcmWAV(depth uint16, rate, frames uint32) []byte {
	block := 2 * depth / 8
	data := frames * uint32(block)
	var b bytes.Buffer
	le := func(vs ...any) {
		for _, v := range vs {
			_ = binary.Write(&b, binary.LittleEndian, v)
		}
	}
	b.WriteString("RIFF")
	le(36 + data)
	b.WriteString("WAVEfmt ")
	le(uint32(16), uint16(1), uint16(2), rate, rate*uint32(block), block, depth)
	b.WriteString("data")
	le(data)
	b.Write(make([]byte, data))
	return b.Bytes()
}

// pcmAIFF returns a silent stereo AIFF of the given bit depth and rate.
func pcmAIFF(depth uint16, rate, frames uint32) []byte {
	data := frames * 2 * uint32(depth) / 8
	// COMM stores the rate as an 80-bit IEEE extended float: normalize the
	// mantissa so its top bit is set.
	exp, mant := uint16(16383+31), uint64(rate)<<32
	for mant&(1<<63) == 0 {
		exp--
		mant <<= 1
	}
	var b bytes.Buffer
	be := func(vs ...any) {
		for _, v := range vs {
			_ = binary.Write(&b, binary.BigEndian, v)
		}
	}
	b.WriteString("FORM")
	be(4 + 26 + 16 + data)
	b.WriteString("AIFFCOMM")
	be(uint32(18), uint16(2), frames, depth, exp, mant)
	b.WriteString("SSND")
	be(8+data, uint32(0), uint32(0))
	b.Write(make([]byte, data))
	return b.Bytes()
}

// WAV and AIFF are decoded at their own bit depth and rate, so a hi-res WAV
// and an AIFF both have to reach EOS. The AIFF is 16-bit: CI has no
// gst-plugins-bad, and gst-libav's avdemux_aiff only covers 16-bit files.
func TestLocalPCMPlaysToEOS(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")
	cases := []struct {
		name string
		data []byte
	}{
		{"hires.wav", pcmWAV(24, 96000, 24000)},
		{"cd.aiff", pcmAIFF(16, 44100, 11025)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.name)
			if err := os.WriteFile(path, c.data, 0o600); err != nil {
				t.Fatalf("writing fixture: %v", err)
			}
			if d := playToEOS(t, path); d > 0 && (d < 240*time.Millisecond || d > 260*time.Millisecond) {
				t.Errorf("Duration = %v, want approximately 250ms", d)
			}
		})
	}
}

func TestLocalAIFFMissingParserHint(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")
	if hasElement("aiffparse") {
		t.Skip("aiffparse installed; this test exercises the gst-libav fallback")
	}
	for _, name := range []string{"hires.aiff", "hires.AIF"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, pcmAIFF(24, 96000, 24000), 0o600); err != nil {
				t.Fatal(err)
			}
			p, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer p.Destroy()
			errors := make(chan error, 1)
			p.OnError(func(err error) {
				select {
				case errors <- err:
				default:
				}
			})
			p.PlayURI("file://" + path)
			select {
			case err := <-errors:
				if !strings.Contains(err.Error(), aiffParserHelp) {
					t.Fatalf("error lacks AIFF plugin hint: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timed out waiting for unsupported AIFF error")
			}
		})
	}
}
