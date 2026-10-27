package browserless

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"
)

// mp3Frame is an MPEG-1 layer III header, 192 kbps, 44.1 kHz, stereo,
// padded out with zeros.
func mp3Frame() []byte {
	f := make([]byte, 64)
	copy(f, []byte{0xff, 0xfb, 0xb0, 0x00})
	return f
}

func TestSniffRawFormatCBRMP3(t *testing.T) {
	// 138.133 s at 192 kbps, the upload checked against Apple for #196.
	size := int64(3315200)
	f, ok := sniffRawFormat(mp3Frame(), size)
	if !ok || f.ext != ".mp3" || f.contentType != "audio/mpeg" || !f.seekable {
		t.Fatalf("sniffRawFormat = %+v, %v", f, ok)
	}
	want := time.Duration(size * 8 * int64(time.Second) / 192000)
	if f.duration != want {
		t.Fatalf("duration = %v, want %v", f.duration, want)
	}
}

func TestSniffRawFormatXingFrameCount(t *testing.T) {
	head := mp3Frame()
	x := 4 + 32 // stereo MPEG-1 side information
	copy(head[x:], "Xing")
	binary.BigEndian.PutUint32(head[x+4:], 1)
	binary.BigEndian.PutUint32(head[x+8:], 1000)
	f, ok := sniffRawFormat(head, 1<<20)
	if !ok {
		t.Fatal("not recognised")
	}
	if want := time.Duration(1000 * 1152 * int64(time.Second) / 44100); f.duration != want {
		t.Fatalf("duration = %v, want %v", f.duration, want)
	}
}

func TestSniffRawFormatSkipsID3(t *testing.T) {
	// A 0x7f-byte tag, then a frame; a stray sync pattern inside the tag
	// must not be taken for the first frame.
	tag := make([]byte, 10+0x7f)
	copy(tag, []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0x7f})
	tag[20], tag[21], tag[22] = 0xff, 0xfb, 0x10
	head := slices.Concat(tag, mp3Frame())
	size := int64(len(tag)) + 192000/8*10
	f, ok := sniffRawFormat(head, size)
	if !ok {
		t.Fatal("not recognised")
	}
	if f.duration != 10*time.Second {
		t.Fatalf("duration = %v, want 10s", f.duration)
	}
}

func TestSniffRawFormatMP4(t *testing.T) {
	head := []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}
	f, ok := sniffRawFormat(head, 1<<20)
	if !ok || f.ext != ".m4a" || f.seekable || f.duration != 0 {
		t.Fatalf("sniffRawFormat = %+v, %v", f, ok)
	}
}

func TestSniffRawFormatRejectsUnknown(t *testing.T) {
	if f, ok := sniffRawFormat([]byte("fLaC\x00\x00\x00\x22"), 1<<20); ok {
		t.Fatalf("sniffRawFormat = %+v, want rejection", f)
	}
}
