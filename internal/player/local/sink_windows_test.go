//go:build windows && cgo

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

// writeTestWAV writes stereo silence at rate with the given formatTag
// (1 = WAVE_FORMAT_PCM, 3 = WAVE_FORMAT_IEEE_FLOAT) and bitsPerSample.
func writeTestWAV(t *testing.T, path string, formatTag uint16, rate uint32, bitsPerSample uint16, frames uint32) {
	t.Helper()
	const channels = 2
	blockAlign := uint16(channels) * (bitsPerSample / 8)
	byteRate := rate * uint32(blockAlign)
	dataSize := frames * uint32(blockAlign)

	var buf bytes.Buffer
	for _, v := range []any{
		[4]byte{'R', 'I', 'F', 'F'}, uint32(36) + dataSize,
		[4]byte{'W', 'A', 'V', 'E'}, [4]byte{'f', 'm', 't', ' '},
		uint32(16), formatTag, uint16(channels), rate, byteRate, blockAlign, bitsPerSample,
		[4]byte{'d', 'a', 't', 'a'}, dataSize,
	} {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	buf.Write(make([]byte, dataSize))
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestNativeSinkPlaysHiResWAV exercises the Windows Media Foundation local
// sink against 24-bit 96 kHz PCM and 32-bit float 96 kHz WAV files, including
// the 10-band biquad equalizer, volume scaling, duration reading, and EOS
// queue advancement.
func TestNativeSinkPlaysHiResWAV(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")

	const (
		rate   = 96000
		frames = rate * 2 / 5 // 400ms
		wantDur = 400 * time.Millisecond
	)
	dir := t.TempDir()
	pcm24Path := filepath.Join(dir, "pcm24_96k.wav")
	flt32Path := filepath.Join(dir, "flt32_96k.wav")
	writeTestWAV(t, pcm24Path, 1, rate, 24, frames) // 24-bit 96 kHz PCM
	writeTestWAV(t, flt32Path, 3, rate, 32, frames) // 32-bit 96 kHz IEEE float

	tracks := []provider.Track{
		{ID: "local:" + pcm24Path, Title: "PCM24"},
		{ID: "local:" + flt32Path, Title: "FLT32"},
	}

	p, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	p.LoadTracks(tracks)
	bands := player.DefaultEQBands()
	bands[0].Gain = 4
	bands[5].Gain = -3
	if err := p.SetEqualizer(bands); err != nil {
		t.Fatal(err)
	}
	if err := p.SetVolume(0.8); err != nil {
		t.Fatal(err)
	}
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}

	s, _ := p.GetState()
	if s.Track == nil || s.Track.Title != "PCM24" {
		t.Fatalf("initial track = %+v, want PCM24", s.Track)
	}
	if s.Track.Duration != wantDur {
		t.Errorf("PCM24 duration = %v, want %v", s.Track.Duration, wantDur)
	}

	// Wait for PCM24 to reach EOS and advance to FLT32.
	waitFor(t, "advance from 24-bit WAV to 32-bit float WAV", func() bool {
		return nowPlaying(t, p) == "FLT32"
	})

	s, _ = p.GetState()
	if s.Track == nil || s.Track.Duration != wantDur {
		t.Errorf("FLT32 duration = %v, want %v", s.Track.Duration, wantDur)
	}

	// Wait for FLT32 to finish the queue cleanly.
	waitFor(t, "32-bit float WAV to finish", func() bool {
		st, _ := p.GetState()
		return !st.Playing && !st.Loading
	})
}
