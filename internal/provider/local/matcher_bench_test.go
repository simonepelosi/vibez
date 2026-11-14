package local_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/provider/local"
)

func writeBenchFLAC(tb testing.TB, path string, d time.Duration, comments map[string]string) {
	tb.Helper()
	var buf bytes.Buffer
	buf.WriteString("fLaC")

	buf.Write([]byte{0x00, 0x00, 0x00, 34})
	var si [34]byte
	const sr = 44100
	totalSamples := uint64(d.Seconds() * sr)
	packed := (uint64(sr)&0xFFFFF)<<44 | (uint64(1)&0x7)<<41 | (uint64(15)&0x1F)<<36 | (totalSamples & 0xFFFFFFFFF)
	binary.BigEndian.PutUint64(si[10:18], packed)
	buf.Write(si[:])

	var vc bytes.Buffer
	const vendor = "vibez-bench"
	_ = binary.Write(&vc, binary.LittleEndian, uint32(len(vendor)))
	vc.WriteString(vendor)
	_ = binary.Write(&vc, binary.LittleEndian, uint32(len(comments))) //nolint:gosec // G115: small map in benchmark fixture
	for k, v := range comments {
		entry := k + "=" + v
		_ = binary.Write(&vc, binary.LittleEndian, uint32(len(entry))) //nolint:gosec // G115: small tag in benchmark fixture
		vc.WriteString(entry)
	}
	vcBytes := vc.Bytes()
	l := len(vcBytes)
	buf.Write([]byte{0x84, byte(l >> 16), byte(l >> 8), byte(l)}) //nolint:gosec // G115: masked 24-bit FLAC metadata block length
	buf.Write(vcBytes)

	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		tb.Fatalf("writeBenchFLAC(%s): %v", path, err)
	}
}

func populateBenchLibrary(tb testing.TB, trackCount int) string {
	tb.Helper()
	dir := tb.TempDir()
	for i := range trackCount {
		artistDir := filepath.Join(dir, fmt.Sprintf("Artist_%02d", i%12), fmt.Sprintf("Album_%02d", i%24))
		if err := os.MkdirAll(artistDir, 0o750); err != nil {
			tb.Fatalf("MkdirAll: %v", err)
		}
		path := filepath.Join(artistDir, fmt.Sprintf("track_%03d.flac", i))
		writeBenchFLAC(tb, path, 210*time.Second, map[string]string{
			"TITLE":  fmt.Sprintf("Track %03d", i),
			"ARTIST": fmt.Sprintf("Artist %02d", i%12),
			"ALBUM":  fmt.Sprintf("Album %02d", i%24),
			"ISRC":   fmt.Sprintf("USVIB26%05d", i),
		})
	}
	return dir
}

func BenchmarkNewMatcherContext(b *testing.B) {
	dir := populateBenchLibrary(b, 120)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		m, err := local.NewMatcherContext(ctx, dir)
		if err != nil {
			b.Fatalf("NewMatcherContext: %v", err)
		}
		if m.Count() != 120 {
			b.Fatalf("matcher count = %d, want 120", m.Count())
		}
	}
}

func BenchmarkProviderNew(b *testing.B) {
	dir := populateBenchLibrary(b, 120)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		p, err := local.New(dir)
		if err != nil {
			b.Fatalf("local.New: %v", err)
		}
		tracks, err := p.GetLibraryTracks(ctx)
		if err != nil {
			b.Fatalf("GetLibraryTracks: %v", err)
		}
		if len(tracks) != 120 {
			b.Fatalf("tracks = %d, want 120", len(tracks))
		}
	}
}
