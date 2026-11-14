package local

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeDuration_OverflowAndBounds(t *testing.T) {
	if got := safeDuration(0, 44100); got != 0 {
		t.Errorf("safeDuration(0, 44100) = %v, want 0", got)
	}
	if got := safeDuration(44100, 0); got != 0 {
		t.Errorf("safeDuration(44100, 0) = %v, want 0", got)
	}
	if got := safeDuration(math.MaxUint64, 1); got != 0 {
		t.Errorf("safeDuration(MaxUint64, 1) = %v, want 0 (overflow)", got)
	}
	want := 3 * time.Second
	if got := safeDuration(44100*3, 44100); got != want {
		t.Errorf("safeDuration(132300, 44100) = %v, want %v", got, want)
	}
}

func TestWalkMP4Boxes_DepthAndSizeBounds(t *testing.T) {
	// Nested moov boxes exceeding maxMP4BoxDepth should terminate safely without stack overflow.
	depth := maxMP4BoxDepth + 10
	buf := make([]byte, depth*8)
	for i := range depth {
		off := i * 8
		rem := uint32(len(buf) - off) //nolint:gosec // small fixed buffer in test
		binary.BigEndian.PutUint32(buf[off:off+4], rem)
		copy(buf[off+4:off+8], "moov")
	}
	var dur time.Duration
	var codec string
	walkMP4Boxes(bytes.NewReader(buf), 0, int64(len(buf)), 0, &dur, &codec)
	if dur != 0 || codec != "" {
		t.Errorf("unexpected result from deeply nested boxes: dur=%v codec=%q", dur, codec)
	}

	// Extended 64-bit box size claiming 0x7fffffffffffffff must be rejected immediately.
	ext := make([]byte, 16)
	binary.BigEndian.PutUint32(ext[0:4], 1)
	copy(ext[4:8], "moov")
	binary.BigEndian.PutUint64(ext[8:16], 0x7fffffffffffffff)
	walkMP4Boxes(bytes.NewReader(ext), 0, int64(len(ext)), 0, &dur, &codec)
	if dur != 0 {
		t.Errorf("expected dur=0 on extended size overflow, got %v", dur)
	}
}

func TestReadWAVDuration_ZeroChunkBound(t *testing.T) {
	// Crafted WAV with zero-sized chunks must exit within maxMetadataBlocks without hanging.
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(100))
	b.WriteString("WAVE")
	for range 20 {
		b.WriteString("JUNK")
		_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	}
	if got := readWAVDuration(bytes.NewReader(b.Bytes())); got != 0 {
		t.Errorf("expected 0 for WAV without fmt/data, got %v", got)
	}
}

func TestOpenRegularAudioFile_RejectsSymlinkAndDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.flac")
	if err := os.WriteFile(target, []byte("notflac"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "link.flac")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if f, err := openRegularAudioFile(link); err == nil {
		_ = f.Close()
		t.Fatal("openRegularAudioFile succeeded on symlink, expected error")
	}
	if f, err := openRegularAudioFile(dir); err == nil {
		_ = f.Close()
		t.Fatal("openRegularAudioFile succeeded on directory, expected error")
	}
	f, err := openRegularAudioFile(target)
	if err != nil {
		t.Fatalf("openRegularAudioFile failed on regular file: %v", err)
	}
	_ = f.Close()
}

func FuzzWalkMP4Boxes(f *testing.F) {
	f.Add([]byte{0, 0, 0, 8, 'f', 't', 'y', 'p'})
	f.Add([]byte{0, 0, 0, 1, 'm', 'o', 'o', 'v', 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(_ *testing.T, data []byte) {
		var dur time.Duration
		var codec string
		walkMP4Boxes(bytes.NewReader(data), 0, int64(len(data)), 0, &dur, &codec)
	})
}

func FuzzReadAudioDurations(f *testing.F) {
	f.Add([]byte("fLaC\x80\x00\x00\x22"))
	f.Add([]byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00"))
	f.Add([]byte("FORM\x00\x00\x00\x20AIFFCOMM\x00\x00\x00\x12"))
	f.Fuzz(func(_ *testing.T, data []byte) {
		r := bytes.NewReader(data)
		_ = readFLACDuration(r)
		_, _ = r.Seek(0, 0)
		_ = readWAVDuration(r)
		_, _ = r.Seek(0, 0)
		_ = readAIFFDuration(r)
		_, _ = r.Seek(0, 0)
		_ = readMP3Duration(r)
		_, _ = r.Seek(0, 0)
		_ = readOGGDuration(r)
	})
}
