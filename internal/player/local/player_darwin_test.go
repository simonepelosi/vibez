//go:build darwin && cgo

package local

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// trackFrames is 0.7s at 44.1kHz: long enough that EOS arrives through the
// queue callback rather than during vibez_start's pre-fill.
const trackFrames = 44100 * 7 / 10

// writeSilentWAV writes trackFrames of 16-bit stereo 44.1kHz silence.
func writeSilentWAV(t *testing.T, path string) {
	t.Helper()
	const data = trackFrames * 4
	var buf bytes.Buffer
	for _, v := range []any{
		[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + data),
		[4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '},
		uint32(16), uint16(1), uint16(2), uint32(44100), uint32(44100 * 4), uint16(4), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, uint32(data),
	} {
		_ = binary.Write(&buf, binary.LittleEndian, v)
	}
	buf.Write(make([]byte, data))
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSwitchNearEndOfTrack switches tracks around the moment each one ends,
// so an outgoing queue regularly reaches EOS after it has been swapped out.
// Before #143 that EOS arrived on a deleted cgo handle and panicked.
func TestSwitchNearEndOfTrack(t *testing.T) {
	dir := t.TempDir()
	var tracks []provider.Track
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		id := "local:" + filepath.Join(dir, name+".wav")
		writeSilentWAV(t, id[len("local:"):])
		tracks = append(tracks, provider.Track{ID: id})
		ids = append(ids, id)
	}

	p, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	p.LoadTracks(tracks)
	_ = p.SetRepeat(player.RepeatModeAll)
	_ = p.SetQueue(ids)
	if s, _ := p.GetState(); s.Error != "" {
		t.Skipf("no audio output available: %s", s.Error)
	}

	// Offsets straddle the 700ms track length, so some switches land just
	// before an EOS and some just after one.
	offsets := []time.Duration{500, 650, 800, 550, 700, 850, 600, 750}
	deadline := time.Now().Add(6 * time.Second)
	for i := 0; time.Now().Before(deadline); i++ {
		time.Sleep(offsets[i%len(offsets)] * time.Millisecond)
		_ = p.Next()
	}
}

// writePCM writes frames of 24-bit stereo silence at rate as a WAV or AIFF
// file, chosen by the path's extension.
func writePCM(t *testing.T, path string, rate uint32, frames uint32) {
	t.Helper()
	data := frames * 6
	var buf bytes.Buffer
	if filepath.Ext(path) == ".wav" {
		for _, v := range []any{
			[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + data),
			[4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '},
			uint32(16), uint16(1), uint16(2), rate, rate * 6, uint16(6), uint16(24),
			[4]byte{'d', 'a', 't', 'a'}, data,
		} {
			_ = binary.Write(&buf, binary.LittleEndian, v)
		}
	} else {
		// AIFF stores the rate as an 80-bit extended float: a biased
		// exponent, then a 64-bit mantissa with an explicit leading one.
		exp := 63
		mant := uint64(rate)
		for mant&(1<<63) == 0 {
			mant <<= 1
			exp--
		}
		for _, v := range []any{
			[4]byte{'F', 'O', 'R', 'M'}, uint32(4 + 26 + 16 + data),
			[4]byte{'A', 'I', 'F', 'F'}, [4]byte{'C', 'O', 'M', 'M'},
			uint32(18), uint16(2), frames, uint16(24), uint16(16383 + exp), mant,
			[4]byte{'S', 'S', 'N', 'D'}, uint32(8 + data), uint32(0), uint32(0),
		} {
			_ = binary.Write(&buf, binary.BigEndian, v)
		}
	}
	buf.Write(make([]byte, data))
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHiResFilesPlayAtTheirOwnRate plays 24-bit 96 kHz WAV and AIFF files and
// checks the reported duration and that the queue's clock keeps to the wall
// clock. The queue runs at the file's own rate, so a format mismatch between
// the decoder and the queue would show up as drift.
func TestHiResFilesPlayAtTheirOwnRate(t *testing.T) {
	const rate = 96000
	for _, name := range []string{"hires.wav", "hires.aiff"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			writePCM(t, path, rate, rate*2)
			id := "local:" + path

			p, err := New(nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = p.Close() }()
			p.LoadTracks([]provider.Track{{ID: id}})
			_ = p.SetQueue([]string{id})
			s, _ := p.GetState()
			if s.Error != "" {
				t.Skipf("no audio output available: %s", s.Error)
			}
			if s.Track == nil {
				t.Fatal("no track playing")
			}
			if s.Track.Duration != 2*time.Second {
				t.Errorf("duration = %v, want 2s", s.Track.Duration)
			}

			time.Sleep(time.Second)
			s, _ = p.GetState()
			if s.Position < 700*time.Millisecond || s.Position > 1300*time.Millisecond {
				t.Errorf("position after 1s = %v, want about 1s", s.Position)
			}
		})
	}
}
