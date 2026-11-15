package local_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/provider/local"
)

// writeSyntheticFLAC writes a minimal valid FLAC file with a STREAMINFO block
// (44.1 kHz, 16-bit stereo, duration d) and a VORBIS_COMMENT block carrying the
// supplied key=value tags.
func writeSyntheticFLAC(t *testing.T, path string, d time.Duration, comments map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("fLaC")

	// STREAMINFO block (type 0, not last, 34 bytes).
	buf.Write([]byte{0x00, 0x00, 0x00, 34})
	var si [34]byte
	const sr = 44100
	totalSamples := uint64(d.Seconds() * sr)
	// Pack sampleRate (20b), channels-1 (3b), bps-1 (5b), totalSamples (36b) into si[10:18].
	packed := (uint64(sr)&0xFFFFF)<<44 | (uint64(1)&0x7)<<41 | (uint64(15)&0x1F)<<36 | (totalSamples & 0xFFFFFFFFF)
	binary.BigEndian.PutUint64(si[10:18], packed)
	buf.Write(si[:])

	// VORBIS_COMMENT block (type 4, last=true -> 0x84).
	var vc bytes.Buffer
	vendor := "vibez-test"
	_ = binary.Write(&vc, binary.LittleEndian, uint32(len(vendor))) //nolint:gosec
	vc.WriteString(vendor)
	_ = binary.Write(&vc, binary.LittleEndian, uint32(len(comments))) //nolint:gosec
	for k, v := range comments {
		entry := k + "=" + v
		_ = binary.Write(&vc, binary.LittleEndian, uint32(len(entry))) //nolint:gosec
		vc.WriteString(entry)
	}
	vcBytes := vc.Bytes()
	l := len(vcBytes)
	buf.Write([]byte{0x84, byte(l >> 16), byte(l >> 8), byte(l)}) //nolint:gosec
	buf.Write(vcBytes)

	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("writeSyntheticFLAC(%s): %v", path, err)
	}
}

// writeSyntheticM4A writes a minimal ISO BMFF (.m4a) container with ftyp,
// moov/mvhd (duration d), moov/trak/mdia/minf/stbl/stsd (codec fourCC "alac" or
// "mp4a"), and moov/udta/meta/ilst with title/artist/album/trkn.
func writeSyntheticM4A(t *testing.T, path, codec string, d time.Duration, title, artist, album string, trackNum int) {
	t.Helper()
	box := func(typ string, body []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(8+len(body))) //nolint:gosec
		b.WriteString(typ)
		b.Write(body)
		return b.Bytes()
	}
	textAtom := func(typ, val string) []byte {
		var data bytes.Buffer
		_ = binary.Write(&data, binary.BigEndian, uint32(1)) // UTF-8
		_ = binary.Write(&data, binary.BigEndian, uint32(0))
		data.WriteString(val)
		return box(typ, box("data", data.Bytes()))
	}
	trknAtom := func(num int) []byte {
		var data bytes.Buffer
		_ = binary.Write(&data, binary.BigEndian, uint32(0))
		_ = binary.Write(&data, binary.BigEndian, uint32(0))
		_ = binary.Write(&data, binary.BigEndian, uint16(0))
		_ = binary.Write(&data, binary.BigEndian, uint16(num)) //nolint:gosec
		_ = binary.Write(&data, binary.BigEndian, uint16(10))
		_ = binary.Write(&data, binary.BigEndian, uint16(0))
		return box("trkn", box("data", data.Bytes()))
	}

	ftyp := box("ftyp", []byte("M4A \x00\x00\x00\x00M4A mp42isom"))

	var mvhdBody [100]byte
	const timescale = 1000
	binary.BigEndian.PutUint32(mvhdBody[12:16], timescale)
	binary.BigEndian.PutUint32(mvhdBody[16:20], uint32(d.Milliseconds())) //nolint:gosec
	mvhd := box("mvhd", mvhdBody[:])

	var stsdBody bytes.Buffer
	stsdBody.Write([]byte{0, 0, 0, 0}) // version/flags
	_ = binary.Write(&stsdBody, binary.BigEndian, uint32(1))
	stsdBody.Write(box(codec, make([]byte, 28)))
	trak := box("trak", box("mdia", box("minf", box("stbl", box("stsd", stsdBody.Bytes())))))

	var ilstBody bytes.Buffer
	ilstBody.Write(textAtom("\xa9nam", title))
	ilstBody.Write(textAtom("\xa9ART", artist))
	ilstBody.Write(textAtom("\xa9alb", album))
	if trackNum > 0 {
		ilstBody.Write(trknAtom(trackNum))
	}
	var metaBody bytes.Buffer
	metaBody.Write([]byte{0, 0, 0, 0})
	metaBody.Write(box("hdlr", make([]byte, 24)))
	metaBody.Write(box("ilst", ilstBody.Bytes()))
	udta := box("udta", box("meta", metaBody.Bytes()))

	var moovBody bytes.Buffer
	moovBody.Write(mvhd)
	moovBody.Write(trak)
	moovBody.Write(udta)
	moov := box("moov", moovBody.Bytes())

	var file bytes.Buffer
	file.Write(ftyp)
	file.Write(moov)
	if err := os.WriteFile(path, file.Bytes(), 0o600); err != nil {
		t.Fatalf("writeSyntheticM4A(%s): %v", path, err)
	}
}

func TestMatcher_MatchesByISRCFirst(t *testing.T) {
	dir := t.TempDir()
	flacPath := filepath.Join(dir, "01 - Everything In Its Right Place.flac")
	writeSyntheticFLAC(t, flacPath, 4*time.Minute+11*time.Second, map[string]string{
		"TITLE":       "Everything In Its Right Place (2000 Release)",
		"ARTIST":      "Radiohead",
		"ALBUM":       "Kid A",
		"TRACKNUMBER": "1",
		"ISRC":        "GB-AYE-00-00351",
	})

	m, err := local.NewMatcher(dir)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if m.Count() != 1 {
		t.Fatalf("Matcher.Count() = %d, want 1", m.Count())
	}

	// Catalog track has different title suffix ("Remastered") and no track
	// number, but matches on normalized ISRC.
	res, ok := m.Match(provider.Track{
		ID:     "1097862870",
		Title:  "Everything In Its Right Place",
		Artist: "Radiohead",
		Album:  "Kid A (Deluxe)",
		ISRC:   "GBAYE0000351",
	})
	if !ok {
		t.Fatal("expected ISRC match, got none")
	}
	if !res.ByISRC {
		t.Error("expected ByISRC = true")
	}
	if res.Path != flacPath {
		t.Errorf("Path = %q, want %q", res.Path, flacPath)
	}
	if res.Format != "FLAC" {
		t.Errorf("Format = %q, want FLAC", res.Format)
	}
}

func TestMatcher_FallbackByMetadataAndDuration(t *testing.T) {
	dir := t.TempDir()
	flacPath := filepath.Join(dir, "02 - Kid A.flac")
	writeSyntheticFLAC(t, flacPath, 4*time.Minute+44*time.Second, map[string]string{
		"TITLE":       "Kid A",
		"ARTIST":      "Radiohead",
		"ALBUM":       "Kid A",
		"TRACKNUMBER": "2",
		"DISCNUMBER":  "1",
	})

	m, err := local.NewMatcher(dir)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	// Within +/-2s tolerance -> matches.
	res, ok := m.Match(provider.Track{
		ID:          "1097862871",
		Title:       "kid a",
		Artist:      "Radiohead",
		Album:       "Kid A",
		TrackNumber: 2,
		DiscNumber:  1,
		Duration:    4*time.Minute + 45*time.Second,
	})
	if !ok {
		t.Fatal("expected fallback metadata match, got none")
	}
	if res.ByISRC {
		t.Error("expected ByISRC = false for fallback match")
	}
	if res.Path != flacPath {
		t.Errorf("Path = %q, want %q", res.Path, flacPath)
	}

	// Outside +/-2s tolerance (e.g. live version 6m10s) -> rejected.
	if _, ok := m.Match(provider.Track{
		ID:          "999",
		Title:       "Kid A",
		Artist:      "Radiohead",
		Album:       "Kid A",
		TrackNumber: 2,
		Duration:    6*time.Minute + 10*time.Second,
	}); ok {
		t.Error("expected duration mismatch to be rejected")
	}

	// Wrong track number -> rejected.
	if _, ok := m.Match(provider.Track{
		ID:          "998",
		Title:       "Kid A",
		Artist:      "Radiohead",
		Album:       "Kid A",
		TrackNumber: 3,
		Duration:    4*time.Minute + 44*time.Second,
	}); ok {
		t.Error("expected track number mismatch to be rejected")
	}
}

func TestMatcher_FallbackRejectsAmbiguousCandidates(t *testing.T) {
	dir := t.TempDir()
	sub1 := filepath.Join(dir, "ripped1")
	sub2 := filepath.Join(dir, "ripped2")
	_ = os.MkdirAll(sub1, 0o750)
	_ = os.MkdirAll(sub2, 0o750)

	tags := map[string]string{
		"TITLE":       "Idioteque",
		"ARTIST":      "Radiohead",
		"ALBUM":       "Kid A",
		"TRACKNUMBER": "8",
	}
	writeSyntheticFLAC(t, filepath.Join(sub1, "08.flac"), 5*time.Minute+9*time.Second, tags)
	writeSyntheticFLAC(t, filepath.Join(sub2, "08.flac"), 5*time.Minute+9*time.Second, tags)

	m, err := local.NewMatcher(dir)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if _, ok := m.Match(provider.Track{
		ID:          "1097862878",
		Title:       "Idioteque",
		Artist:      "Radiohead",
		Album:       "Kid A",
		TrackNumber: 8,
		Duration:    5*time.Minute + 9*time.Second,
	}); ok {
		t.Error("expected ambiguous fallback (2 candidates) to return false")
	}
}

func TestMatcher_IndexesALACButIgnoresLossyAACAndMP3(t *testing.T) {
	dir := t.TempDir()
	alacPath := filepath.Join(dir, "alac.m4a")
	aacPath := filepath.Join(dir, "aac.m4a")
	mp3Path := filepath.Join(dir, "lossy.mp3")

	writeSyntheticM4A(t, alacPath, "alac", 3*time.Minute+30*time.Second, "Nude", "Radiohead", "In Rainbows", 3)
	writeSyntheticM4A(t, aacPath, "mp4a", 3*time.Minute+50*time.Second, "15 Step", "Radiohead", "In Rainbows", 1)
	if err := os.WriteFile(mp3Path, []byte("ID3\x03\x00\x00\x00\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := local.NewMatcher(dir)
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}
	if m.Count() != 1 {
		t.Fatalf("Matcher.Count() = %d, want 1 (only ALAC .m4a)", m.Count())
	}

	res, ok := m.Match(provider.Track{
		ID:          "111",
		Title:       "Nude",
		Artist:      "Radiohead",
		Album:       "In Rainbows",
		TrackNumber: 3,
		Duration:    3*time.Minute + 30*time.Second,
	})
	if !ok {
		t.Fatal("expected ALAC .m4a to match")
	}
	if res.Format != "ALAC" {
		t.Errorf("Format = %q, want ALAC", res.Format)
	}

	if _, ok := m.Match(provider.Track{
		ID:          "222",
		Title:       "15 Step",
		Artist:      "Radiohead",
		Album:       "In Rainbows",
		TrackNumber: 1,
		Duration:    3*time.Minute + 50*time.Second,
	}); ok {
		t.Error("expected lossy AAC .m4a to be ignored by lossless matcher")
	}
}

func TestMatcher_ISRCDurationAgreementAndRejection(t *testing.T) {
	t.Run("rejects ISRC candidate with disagreeing duration", func(t *testing.T) {
		dir := t.TempDir()
		livePath := filepath.Join(dir, "live.flac")
		writeSyntheticFLAC(t, livePath, 6*time.Minute+30*time.Second, map[string]string{
			"TITLE":       "Track",
			"ARTIST":      "Artist",
			"ALBUM":       "Live Album",
			"TRACKNUMBER": "1",
			"ISRC":        "USRC11111111",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		if res, ok := m.Match(provider.Track{
			ID:       "1",
			Title:    "Track",
			Artist:   "Artist",
			Album:    "Studio Album",
			Duration: 3 * time.Minute,
			ISRC:     "USRC11111111",
		}); ok {
			t.Errorf("expected ISRC mismatch (>2s) to be rejected, matched %+v", res)
		}
	})

	t.Run("picks duration-matching ISRC candidate over mismatched one", func(t *testing.T) {
		dir := t.TempDir()
		subA := filepath.Join(dir, "a")
		subB := filepath.Join(dir, "b")
		_ = os.MkdirAll(subA, 0o750)
		_ = os.MkdirAll(subB, 0o750)
		wrongPath := filepath.Join(subA, "clean.flac")
		rightPath := filepath.Join(subB, "album.flac")
		writeSyntheticFLAC(t, wrongPath, 2*time.Minute+40*time.Second, map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "ALBUM": "Single", "ISRC": "USRC22222222",
		})
		writeSyntheticFLAC(t, rightPath, 4*time.Minute+5*time.Second, map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "ALBUM": "LP", "ISRC": "USRC22222222",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		res, ok := m.Match(provider.Track{
			ID:       "2",
			Title:    "Track",
			Artist:   "Artist",
			Duration: 4*time.Minute + 6*time.Second,
			ISRC:     "USRC22222222",
		})
		if !ok || res.Path != rightPath || !res.ByISRC {
			t.Fatalf("Match = (%+v, %v), want rightPath via ISRC", res, ok)
		}
	})

	t.Run("falls back to metadata when ISRC candidate duration disagrees", func(t *testing.T) {
		dir := t.TempDir()
		wrongISRC := filepath.Join(dir, "01 - mistagged.flac")
		rightMeta := filepath.Join(dir, "02 - studio.flac")
		writeSyntheticFLAC(t, wrongISRC, 7*time.Minute, map[string]string{
			"TITLE": "Live Bonus", "ARTIST": "Band", "ALBUM": "Live", "TRACKNUMBER": "1", "ISRC": "USRC33333333",
		})
		writeSyntheticFLAC(t, rightMeta, 3*time.Minute+20*time.Second, map[string]string{
			"TITLE": "Song", "ARTIST": "Band", "ALBUM": "Record", "TRACKNUMBER": "2",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		res, ok := m.Match(provider.Track{
			ID:          "3",
			Title:       "Song",
			Artist:      "Band",
			Album:       "Record",
			TrackNumber: 2,
			Duration:    3*time.Minute + 20*time.Second,
			ISRC:        "USRC33333333",
		})
		if !ok || res.Path != rightMeta || res.ByISRC {
			t.Fatalf("Match = (%+v, %v), want fallback metadata match to rightMeta", res, ok)
		}
	})

	t.Run("accepts ISRC candidate with unknown duration", func(t *testing.T) {
		dir := t.TempDir()
		unknownPath := filepath.Join(dir, "unknown.flac")
		writeSyntheticFLAC(t, unknownPath, 0, map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "ISRC": "USRC44444444",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		res, ok := m.Match(provider.Track{
			ID:       "4",
			Title:    "Track",
			Artist:   "Artist",
			Duration: 3 * time.Minute,
			ISRC:     "USRC44444444",
		})
		if !ok || !res.ByISRC || res.Path != unknownPath {
			t.Fatalf("Match = (%+v, %v), want unknown-duration ISRC candidate accepted", res, ok)
		}
	})
}

func TestMatcher_NewMatcherContextCancelled(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := local.NewMatcherContext(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("NewMatcherContext(cancelled) = %v, want context.Canceled", err)
	}
}

func TestScan_IncrementalCacheReuseAndInvalidation(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("HOME", cacheDir)
	t.Setenv("LOCALAPPDATA", cacheDir)

	dir := t.TempDir()
	trackA := filepath.Join(dir, "a.flac")
	trackB := filepath.Join(dir, "b.flac")
	writeSyntheticFLAC(t, trackA, 180*time.Second, map[string]string{
		"TITLE": "Title A", "ARTIST": "Artist A", "ISRC": "USRC00000001",
	})
	writeSyntheticFLAC(t, trackB, 200*time.Second, map[string]string{
		"TITLE": "Title B", "ARTIST": "Artist B", "ISRC": "USRC00000002",
	})

	// 1. Cold scan populates the disk cache.
	p1, err := local.New(dir)
	if err != nil {
		t.Fatalf("cold local.New: %v", err)
	}
	tracks1, _ := p1.GetLibraryTracks(context.Background())
	if len(tracks1) != 2 {
		t.Fatalf("cold tracks = %d, want 2", len(tracks1))
	}

	// 2. Corrupt file content of trackB while restoring original size/mtime?
	// Even better: truncate+re-stat shows invalidation, while keeping mtime+size unchanged proves cache hit:
	infoB, err := os.Stat(trackB)
	if err != nil {
		t.Fatal(err)
	}
	origBytesB, err := os.ReadFile(trackB) //nolint:gosec // G304: test temp file path created above
	if err != nil {
		t.Fatal(err)
	}
	// Replace trackB bytes with zeroes of identical length and identical mtime:
	// only an incremental cache hit preserves "Title B" without re-parsing the zeroed file.
	if err := os.WriteFile(trackB, make([]byte, len(origBytesB)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(trackB, infoB.ModTime(), infoB.ModTime()); err != nil {
		t.Fatal(err)
	}

	// Modify trackA with a longer title & new mtime to prove cache invalidation picks up changes.
	writeSyntheticFLAC(t, trackA, 185*time.Second, map[string]string{
		"TITLE": "Updated Title A", "ARTIST": "Artist A", "ISRC": "USRC00000001",
	})
	future := infoB.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(trackA, future, future); err != nil {
		t.Fatal(err)
	}

	p2, err := local.New(dir)
	if err != nil {
		t.Fatalf("warm local.New: %v", err)
	}
	tracks2, _ := p2.GetLibraryTracks(context.Background())
	byPath := map[string]provider.Track{}
	for _, tr := range tracks2 {
		byPath[tr.LocalPath] = tr
	}
	if got := byPath[trackA].Title; got != "Updated Title A" {
		t.Errorf("updated file title = %q, want %q", got, "Updated Title A")
	}
	if got := byPath[trackB].Title; got != "Title B" {
		t.Errorf("cached file title = %q, want %q (expected cache hit)", got, "Title B")
	}

	// 3. Removing a file must prune it from subsequent scans and matcher results.
	if err := os.Remove(trackB); err != nil {
		t.Fatal(err)
	}
	m, err := local.NewMatcher(dir)
	if err != nil {
		t.Fatalf("NewMatcher after delete: %v", err)
	}
	if m.Count() != 1 {
		t.Errorf("matcher count after delete = %d, want 1", m.Count())
	}
}

func TestScan_ContinuesWhenSingleFileUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX chmod 0000 does not block file reads on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses 0000 file permissions")
	}
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("HOME", cacheDir)

	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.flac")
	badPath := filepath.Join(dir, "unreadable.flac")
	writeSyntheticFLAC(t, goodPath, 150*time.Second, map[string]string{"TITLE": "Good Track"})
	writeSyntheticFLAC(t, badPath, 150*time.Second, map[string]string{"TITLE": "Unreadable Track"})
	if err := os.Chmod(badPath, 0o000); err != nil {
		t.Fatalf("chmod 0000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badPath, 0o600) })

	p, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New failed despite readable sibling: %v", err)
	}
	tracks, _ := p.GetLibraryTracks(context.Background())
	if len(tracks) != 1 || tracks[0].Title != "Good Track" {
		t.Fatalf("tracks = %+v, want only Good Track", tracks)
	}
}

func TestScan_SanitizesTamperedCacheEntries(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("HOME", cacheDir)
	t.Setenv("LOCALAPPDATA", cacheDir)

	dir := t.TempDir()
	trackPath := filepath.Join(dir, "a.flac")
	writeSyntheticFLAC(t, trackPath, 180*time.Second, map[string]string{"TITLE": "Original Title"})

	p1, err := local.New(dir)
	if err != nil {
		t.Fatalf("cold local.New: %v", err)
	}
	coldTracks, _ := p1.GetLibraryTracks(context.Background())
	if len(coldTracks) != 1 {
		t.Fatalf("cold tracks len = %d, want 1", len(coldTracks))
	}
	expectedPath := coldTracks[0].LocalPath

	var cacheFile string
	_ = filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			cacheFile = path
		}
		return nil
	})
	if cacheFile == "" {
		t.Fatalf("expected scan cache JSON under %s", cacheDir)
	}

	raw, err := os.ReadFile(cacheFile) //nolint:gosec // G304: cache file created inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	entries := payload["entries"].(map[string]any)
	for _, v := range entries {
		entry := v.(map[string]any)
		trackMap := entry["track"].(map[string]any)
		trackMap["ID"] = "local:/etc/passwd"
		trackMap["LocalPath"] = "/etc/passwd"
		trackMap["Title"] = "\x1b[31mInjected Title\x1b[0m"
		trackMap["Genres"] = []any{"\x1b[2JAmbient"}
	}
	tampered, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	p2, err := local.New(dir)
	if err != nil {
		t.Fatalf("warm local.New: %v", err)
	}
	tracks, _ := p2.GetLibraryTracks(context.Background())
	if len(tracks) != 1 {
		t.Fatalf("tracks len = %d, want 1", len(tracks))
	}
	got := tracks[0]
	if got.LocalPath != expectedPath || got.ID != "local:"+expectedPath {
		t.Errorf("tampered cache overrode path/ID: id=%q path=%q want=%q", got.ID, got.LocalPath, expectedPath)
	}
	if strings.Contains(got.Title, "\x1b") || got.Title != "Injected Title" {
		t.Errorf("unsanitized cached title = %q, want Injected Title", got.Title)
	}
	if len(got.Genres) != 1 || strings.Contains(got.Genres[0], "\x1b") {
		t.Errorf("unsanitized cached genres = %v", got.Genres)
	}
}

func TestScanCache_RejectsWrongDirWorldWritableAndSanitizesAllFields(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("HOME", cacheDir)
	t.Setenv("LOCALAPPDATA", cacheDir)

	dir := t.TempDir()
	flacPath := filepath.Join(dir, "song.flac")
	writeSyntheticFLAC(t, flacPath, 180*time.Second, map[string]string{
		"TITLE": "Clean Song", "ARTIST": "Clean Artist", "ALBUM": "Clean Album",
	})

	// Cold scan writes the verified cache file.
	if _, err := local.New(dir); err != nil {
		t.Fatalf("cold local.New: %v", err)
	}
	var cacheFile string
	_ = filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			cacheFile = path
		}
		return nil
	})
	if cacheFile == "" {
		t.Fatal("expected cache file to be written")
	}
	// Cache file name must use full 256-bit SHA-256 hex digest (64 hex chars).
	base := filepath.Base(cacheFile)
	hexPart := strings.TrimSuffix(strings.TrimPrefix(base, "local-scan-"), ".json")
	if len(hexPart) != 64 {
		t.Fatalf("cache hash hex len = %d (%q), want 64", len(hexPart), base)
	}

	raw, err := os.ReadFile(cacheFile) //nolint:gosec // G304: cache file created inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	entries := payload["entries"].(map[string]any)
	for _, v := range entries {
		tr := v.(map[string]any)["track"].(map[string]any)
		tr["ArtworkURL"] = "http://127.0.0.1:9999/ssrf.jpg"
		tr["PreviewURL"] = "http://169.254.169.254/latest"
		tr["CatalogID"] = "\x1b[31m999"
		tr["ReleaseDate"] = "\x1b[2J2020"
		tr["ContentRating"] = "\x1b[31mexplicit"
		tr["AudioVariants"] = []any{"\x1b[31mdolby-atmos"}
		tr["ArtworkPalette"] = map[string]any{"BgColor": "\x1b[31m#000000"}
		tr["LocalFormat"] = "\x1b[31mALAC"
		tr["ISRC"] = "usrc-12345678\x1b[0m"
	}
	tampered, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheFile, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	p2, err := local.New(dir)
	if err != nil {
		t.Fatalf("warm local.New: %v", err)
	}
	tracks, _ := p2.GetLibraryTracks(context.Background())
	if len(tracks) != 1 {
		t.Fatalf("tracks len = %d, want 1", len(tracks))
	}
	got := tracks[0]
	if got.ArtworkURL != "" || got.PreviewURL != "" || got.CatalogID != "" ||
		got.ReleaseDate != "" || got.ContentRating != "" || len(got.AudioVariants) != 0 ||
		got.ArtworkPalette != (provider.ArtworkPalette{}) {
		t.Fatalf("remote/catalog fields leaked from cache: %+v", got)
	}
	if got.LocalFormat != "FLAC" {
		t.Errorf("LocalFormat = %q, want FLAC (re-validated from extension)", got.LocalFormat)
	}

	// Mismatched dir field in cache JSON must force cache rejection and full re-parse.
	info, err := os.Stat(flacPath)
	if err != nil {
		t.Fatal(err)
	}
	origFLAC, err := os.ReadFile(flacPath) //nolint:gosec // G304: test temp file path created above
	if err != nil {
		t.Fatal(err)
	}
	// Replace file contents with an updated title while holding size/mtime? Simpler: check
	// title restored to real disk tag after zeroed-vs-mismatch test or write wrong dir + title.
	for _, v := range entries {
		v.(map[string]any)["track"].(map[string]any)["Title"] = "Poisoned Wrong Dir"
	}
	payload["dir"] = filepath.Join(dir, "other")
	wrongDirBytes, _ := json.Marshal(payload)
	if err := os.WriteFile(cacheFile, wrongDirBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	pWrongDir, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New(wrong dir cache): %v", err)
	}
	wrongDirTracks, _ := pWrongDir.GetLibraryTracks(context.Background())
	if len(wrongDirTracks) != 1 || wrongDirTracks[0].Title != "Clean Song" {
		t.Fatalf("wrong dir cache was trusted! got=%+v", wrongDirTracks)
	}

	// On Unix, world-writable cache files must also be ignored.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		_ = origFLAC
		payload["dir"] = dir
		for _, v := range entries {
			v.(map[string]any)["track"].(map[string]any)["Title"] = "Poisoned World Writable"
		}
		wwBytes, _ := json.Marshal(payload)
		if err := os.WriteFile(cacheFile, wwBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(cacheFile, 0o666); err != nil { //nolint:gosec // G302: intentionally creating world-writable file in security test
			t.Fatal(err)
		}
		_ = os.Chtimes(flacPath, info.ModTime(), info.ModTime())
		pWW, err := local.New(dir)
		if err != nil {
			t.Fatalf("local.New(world-writable cache): %v", err)
		}
		wwTracks, _ := pWW.GetLibraryTracks(context.Background())
		if len(wwTracks) != 1 || wwTracks[0].Title != "Clean Song" {
			t.Fatalf("world-writable cache was trusted! got=%+v", wwTracks)
		}
	}
}

func TestMatcher_ISRCWildcardAndMinInt64Overflow(t *testing.T) {
	t.Run("rejects unknown-duration ISRC wildcard with conflicting metadata", func(t *testing.T) {
		dir := t.TempDir()
		badPath := filepath.Join(dir, "spoof.flac")
		writeSyntheticFLAC(t, badPath, 0, map[string]string{
			"TITLE": "Completely Different Track", "ARTIST": "Impostor", "ISRC": "USRC55555555",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		if res, ok := m.Match(provider.Track{
			ID:       "1",
			Title:    "Real Song",
			Artist:   "Real Artist",
			Duration: 3 * time.Minute,
			ISRC:     "USRC55555555",
		}); ok {
			t.Fatalf("expected conflicting unknown-duration ISRC candidate to be rejected, got %+v", res)
		}
	})

	t.Run("rejects ambiguous ISRC candidates when want duration is missing", func(t *testing.T) {
		dir := t.TempDir()
		sub1 := filepath.Join(dir, "d1")
		sub2 := filepath.Join(dir, "d2")
		_ = os.MkdirAll(sub1, 0o750)
		_ = os.MkdirAll(sub2, 0o750)
		writeSyntheticFLAC(t, filepath.Join(sub1, "a.flac"), 3*time.Minute, map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "ISRC": "USRC66666666",
		})
		writeSyntheticFLAC(t, filepath.Join(sub2, "b.flac"), 5*time.Minute, map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "ISRC": "USRC66666666",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		if res, ok := m.Match(provider.Track{
			ID:     "2",
			Title:  "Track",
			Artist: "Artist",
			ISRC:   "USRC66666666",
		}); ok {
			t.Fatalf("expected ambiguous ISRC without duration to be rejected, got %+v", res)
		}
	})

	t.Run("handles MinInt64 duration subtraction without wraparound match", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "track.flac")
		writeSyntheticFLAC(t, p, 3*time.Minute, map[string]string{
			"TITLE": "Track", "ARTIST": "Artist", "ISRC": "USRC77777777",
		})
		m, err := local.NewMatcher(dir)
		if err != nil {
			t.Fatalf("NewMatcher: %v", err)
		}
		if res, ok := m.Match(provider.Track{
			ID:       "3",
			Title:    "Track",
			Artist:   "Artist",
			Duration: math.MaxInt64,
			ISRC:     "USRC77777777",
		}); ok {
			t.Fatalf("expected extreme MaxInt64 duration mismatch to be rejected, got %+v", res)
		}
	})
}

func TestTrackFromFile_FLACPictureAllocDoSAndPanicSafety(t *testing.T) {
	dir := t.TempDir()
	evilPath := filepath.Join(dir, "giant-pic.flac")

	// Craft a 100-byte FLAC whose PICTURE metadata block claims a 0xFFFFFFFF data length,
	// which would otherwise cause dhowden/tag/vorbis.go:make([]byte, dataLen) to OOM/panic.
	var buf bytes.Buffer
	buf.WriteString("fLaC")
	// Minimal PICTURE block: type(4)=3, mimeLen(4)=0, descLen(4)=0, w/h/d/c(16)=0, dataLen(4)=0xFFFFFFFF.
	var pic [32]byte
	binary.BigEndian.PutUint32(pic[0:4], 3)
	binary.BigEndian.PutUint32(pic[28:32], 0xFFFFFFFF)
	buf.Write([]byte{0x86, 0x00, 0x00, byte(len(pic))})
	buf.Write(pic[:])
	if err := os.WriteFile(evilPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New failed on adversarial FLAC: %v", err)
	}
	tracks, _ := p.GetLibraryTracks(context.Background())
	if len(tracks) != 1 || tracks[0].Title != "giant-pic.flac" {
		t.Fatalf("tracks = %+v, want safe fallback to filename", tracks)
	}
}

func TestScanNotice_SanitizesPathAndExtension(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filenames with ESC are invalid on Windows")
	}
	parent := t.TempDir()
	evilDir := filepath.Join(parent, "music\x1b[31m")
	if err := os.MkdirAll(evilDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evilDir, "note.txt\x1b[2J"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := local.New(evilDir)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	notice := p.ScanNotice()
	if strings.Contains(notice, "\x1b") {
		t.Fatalf("ScanNotice contains unsanitized ESC: %q", notice)
	}
}
