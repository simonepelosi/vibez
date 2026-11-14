//go:build windows && cgo

package local

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func flacCRC8(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
		for range 8 {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func flacCRC16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// writeTestFLAC24 writes stereo 24-bit silent FLAC frames at rate for the given
// sample count using CONSTANT subframes so Media Foundation decodes a real
// compressed 24-bit stream without external test fixtures.
func writeTestFLAC24(t *testing.T, path string, rate uint32, frames uint32) {
	t.Helper()
	const (
		channels  = 2
		bps       = 24
		blockSize = 3840
	)
	var out bytes.Buffer
	out.WriteString("fLaC")
	// Last metadata block (0x80), STREAMINFO (type 0), 34 bytes.
	out.Write([]byte{0x80, 0x00, 0x00, 34})

	var si [34]byte
	binary.BigEndian.PutUint16(si[0:2], blockSize)
	binary.BigEndian.PutUint16(si[2:4], blockSize)
	packed := (uint64(rate) << 44) |
		(uint64(channels-1) << 41) |
		(uint64(bps-1) << 36) |
		uint64(frames)
	binary.BigEndian.PutUint64(si[10:18], packed)

	// Populate the unencoded-PCM MD5 signature and exact min/max frame sizes
	// so Media Foundation's FLAC parser treats STREAMINFO as complete.
	h := md5.New()
	zeros := make([]byte, blockSize*channels*(bps/8))
	remBytes := int(frames) * channels * (bps / 8)
	for remBytes > 0 {
		chunk := len(zeros)
		if remBytes < chunk {
			chunk = remBytes
		}
		h.Write(zeros[:chunk])
		remBytes -= chunk
	}
	copy(si[18:34], h.Sum(nil))

	srCode := byte(0x00)
	if rate == 96000 {
		srCode = 0x0b
	}

	var framesBuf bytes.Buffer
	var minFrameSize, maxFrameSize uint32
	numBlocks := (frames + blockSize - 1) / blockSize
	for i := uint32(0); i < numBlocks; i++ {
		n := uint32(blockSize)
		if rem := frames - i*blockSize; rem < n {
			n = rem
		}
		var frame bytes.Buffer
		frame.Write([]byte{0xFF, 0xF8, 0x70 | srCode, 0x1C})
		frame.WriteByte(byte(i))
		_ = binary.Write(&frame, binary.BigEndian, uint16(n-1))
		frame.WriteByte(flacCRC8(frame.Bytes()))
		for range channels {
			frame.WriteByte(0x00) // CONSTANT subframe
			frame.Write([]byte{0x00, 0x00, 0x00})
		}
		_ = binary.Write(&frame, binary.BigEndian, flacCRC16(frame.Bytes()))
		fl := uint32(frame.Len())
		if minFrameSize == 0 || fl < minFrameSize {
			minFrameSize = fl
		}
		if fl > maxFrameSize {
			maxFrameSize = fl
		}
		framesBuf.Write(frame.Bytes())
	}

	si[4], si[5], si[6] = byte(minFrameSize>>16), byte(minFrameSize>>8), byte(minFrameSize)
	si[7], si[8], si[9] = byte(maxFrameSize>>16), byte(maxFrameSize>>8), byte(maxFrameSize)
	out.Write(si[:])
	out.Write(framesBuf.Bytes())
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestNativeSinkPlaysHiResWAV exercises the Windows Media Foundation local
// sink against 24-bit 96 kHz PCM WAV, 32-bit float 96 kHz WAV, and 24-bit
// 96 kHz FLAC files, verifying the negotiated PCM output layout alongside EQ,
// volume, duration, and EOS queue advancement.
func TestNativeSinkPlaysHiResWAV(t *testing.T) {
	t.Setenv("VIBEZ_NULL_AUDIO", "1")

	const (
		rate    = 96000
		frames  = rate * 2 / 5 // 400ms
		wantDur = 400 * time.Millisecond
	)
	dir := t.TempDir()
	pcm24Path := filepath.Join(dir, "pcm24_96k.wav")
	flt32Path := filepath.Join(dir, "flt32_96k.wav")
	flac24Path := filepath.Join(dir, "flac24_96k.flac")
	writeTestWAV(t, pcm24Path, 1, rate, 24, frames) // 24-bit 96 kHz PCM
	writeTestWAV(t, flt32Path, 3, rate, 32, frames) // 32-bit 96 kHz IEEE float
	writeTestFLAC24(t, flac24Path, rate, frames)    // 24-bit 96 kHz FLAC

	tracks := []provider.Track{
		{ID: "local:" + pcm24Path, Title: "PCM24"},
		{ID: "local:" + flt32Path, Title: "FLT32"},
		{ID: "local:" + flac24Path, Title: "FLAC24"},
	}

	var logMu sync.Mutex
	var logs []string
	p, err := New(func(msg string) {
		logMu.Lock()
		logs = append(logs, msg)
		logMu.Unlock()
	})
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

	if got := p.sampleFormat(); got == "" || got == "S16" {
		t.Errorf("PCM24 negotiated sample format = %q, want non-16-bit (F32 or S24)", got)
	}

	s, _ := p.GetState()
	if s.Track == nil || s.Track.Title != "PCM24" {
		t.Fatalf("initial track = %+v, want PCM24", s.Track)
	}
	if s.Track.Duration != wantDur {
		t.Errorf("PCM24 duration = %v, want %v", s.Track.Duration, wantDur)
	}

	// Wait for PCM24 to reach EOS and FLT32 to finish loading.
	waitFor(t, "advance from 24-bit WAV to 32-bit float WAV", func() bool {
		st, _ := p.GetState()
		return st.Track != nil && st.Track.Title == "FLT32" && !st.Loading
	})

	s, _ = p.GetState()
	if s.Track == nil || s.Track.Duration != wantDur {
		t.Errorf("FLT32 duration = %v, want %v", s.Track.Duration, wantDur)
	}

	// Wait for FLT32 to reach EOS and FLAC24 to finish loading.
	waitFor(t, "advance from 32-bit float WAV to 24-bit FLAC", func() bool {
		st, _ := p.GetState()
		return st.Track != nil && st.Track.Title == "FLAC24" && !st.Loading
	})

	s, _ = p.GetState()
	if s.Track == nil || s.Track.Duration != wantDur {
		t.Errorf("FLAC24 duration = %v, want %v", s.Track.Duration, wantDur)
	}

	// Wait for FLAC24 to finish the queue cleanly.
	waitFor(t, "24-bit FLAC to finish", func() bool {
		st, _ := p.GetState()
		return !st.Playing && !st.Loading
	})

	logMu.Lock()
	gotLogs := append([]string(nil), logs...)
	logMu.Unlock()
	t.Logf("negotiated logs: %v", gotLogs)

	wantLogFor := func(file string) string {
		for _, l := range gotLogs {
			if strings.Contains(l, "("+file+")") {
				return l
			}
		}
		t.Fatalf("missing audio output log for %s in %v", file, gotLogs)
		return ""
	}
	if l := wantLogFor("pcm24_96k.wav"); !strings.Contains(l, "F32") && !strings.Contains(l, "S24") {
		t.Errorf("pcm24_96k.wav log = %q, want F32 or S24", l)
	}
	if l := wantLogFor("flt32_96k.wav"); !strings.Contains(l, "F32") {
		t.Errorf("flt32_96k.wav log = %q, want F32", l)
	}
	if l := wantLogFor("flac24_96k.flac"); !strings.Contains(l, "F32") && !strings.Contains(l, "S24") {
		t.Errorf("flac24_96k.flac log = %q, want F32 or S24 (compressed decoder fell back to 16-bit)", l)
	}
}
