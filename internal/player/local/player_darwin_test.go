//go:build darwin

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

	p, err := New()
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
