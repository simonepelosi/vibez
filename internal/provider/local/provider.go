// Package local provides a Provider that serves tracks from a local music
// directory. It scans recursively for MP3, FLAC, M4A (plus OGG on Linux, and
// WAV/AIFF on Linux and macOS) and reads their metadata using the dhowden/tag
// library. No network or credentials are required.

package local

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dhowden/tag"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Provider scans a local directory and exposes tracks via the provider interface.
type Provider struct {
	dir    string
	tracks []provider.Track

	// skipped counts the files the last scan ignored for an unsupported
	// extension, keyed by that extension ("" for a file that has none).
	// unreadable counts files with a supported extension that would not open.
	skipped    map[string]int
	unreadable int
}

// New creates a Provider and performs the initial directory scan.
func New(dir string) (*Provider, error) {
	p := &Provider{dir: dir}
	if err := p.scan(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Provider) Name() string          { return "Local" }
func (p *Provider) IsAuthenticated() bool { return true }

// scan walks dir recursively and indexes all regular supported audio files,
// skipping symlinks and special filesystem entries such as FIFOs and devices.
func (p *Provider) scan() error {
	res, err := scanDirectory(context.Background(), p.dir, false)
	if err != nil {
		p.tracks = nil
		p.skipped = map[string]int{}
		p.unreadable = 0
		return err
	}
	p.tracks = res.tracks
	p.skipped = res.skipped
	p.unreadable = res.unreadable
	return nil
}

func canonicalMusicDir(dir string) string {
	cleaned := filepath.Clean(dir)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if info, err := os.Lstat(cleaned); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
			return resolved
		}
	}
	return cleaned
}

func openRegularAudioFile(path string) (*os.File, error) {
	linfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !linfo.Mode().IsRegular() {
		return nil, errors.New("skipping non-regular file")
	}
	f, err := os.Open(path) //nolint:gosec // path is a verified regular file under music dir
	if err != nil {
		return nil, err
	}
	finfo, err := f.Stat()
	if err != nil || !finfo.Mode().IsRegular() || !os.SameFile(linfo, finfo) {
		_ = f.Close()
		return nil, errors.New("file changed or is not a regular file")
	}
	return f, nil
}

// ScanNotice reports what the last scan left out, or "" when every file in the
// directory was indexed. An empty library reaches the TUI looking the same
// whether the directory holds nothing, holds only formats this platform cannot
// play, or holds files that would not open, and only the format case has a
// remedy the user can act on. The supported set is not documented anywhere
// else a user would look, so the message names it.
func (p *Provider) ScanNotice() string {
	skipped := 0
	for _, n := range p.skipped {
		skipped += n
	}
	if skipped == 0 && p.unreadable == 0 && len(p.tracks) > 0 {
		return ""
	}

	dir := provider.Clean(shortPath(p.dir))
	head := "no playable tracks in " + dir
	if len(p.tracks) > 0 {
		head = fmt.Sprintf("indexed %d of %d files in %s",
			len(p.tracks), len(p.tracks)+skipped+p.unreadable, dir)
	}

	var parts []string
	if skipped > 0 {
		// The playable set comes before the detail: a status bar truncates
		// from the right, and it is the half the user can act on.
		parts = append(parts,
			platformName+" plays "+supportedExtList(),
			fmt.Sprintf("skipped %s (%s)", fileCount(skipped), skippedExtList(p.skipped)))
	}
	if p.unreadable > 0 {
		parts = append(parts, fileCount(p.unreadable)+" could not be read")
	}
	if len(parts) == 0 {
		return head + ": the directory is empty"
	}
	return head + ": " + strings.Join(parts, "; ")
}

// fileCount renders a count of files with its plural.
func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// skippedExtList names the skipped extensions, commonest first, capped so the
// line stays inside a status bar. A music directory routinely holds artwork,
// cue sheets and logs beside the audio, so the list can be long and the tail
// of it is never the answer.
func skippedExtList(counts map[string]int) string {
	exts := slices.Sorted(maps.Keys(counts))
	slices.SortStableFunc(exts, func(a, b string) int { return cmp.Compare(counts[b], counts[a]) })

	const named = 3
	parts := make([]string, 0, named+1)
	for i, ext := range exts {
		if i == named {
			parts = append(parts, fmt.Sprintf("+%d more", len(exts)-named))
			break
		}
		name := provider.Clean(ext)
		if name == "" {
			name = "no extension"
		}
		parts = append(parts, fmt.Sprintf("%d %s", counts[ext], name))
	}
	return strings.Join(parts, ", ")
}

// shortPath swaps the home directory for ~. A music directory is usually
// under it, and the path is the one part of the line the user already knows.
func shortPath(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if strings.HasPrefix(dir, home+string(filepath.Separator)) {
		return "~" + dir[len(home):]
	}
	return dir
}

// supportedExtList names this platform's playable extensions.
func supportedExtList() string {
	return strings.Join(slices.Sorted(maps.Keys(supportedExts)), ", ")
}

// trackFromFile reads metadata from an audio file and returns a Track.
func trackFromFile(path string) (provider.Track, error) {
	f, err := openRegularAudioFile(path)
	if err != nil {
		return provider.Track{}, err
	}
	defer func() { _ = f.Close() }()

	m, err := safeReadTags(f)

	title := filepath.Base(path)
	artist := "Unknown Artist"
	album := "Unknown Album"
	var genres []string
	var trackNum, discNum int
	var isrc string

	if err == nil {
		if m.Title() != "" {
			title = m.Title()
		}
		if m.Artist() != "" {
			artist = m.Artist()
		}
		if m.Album() != "" {
			album = m.Album()
		}
		if m.Genre() != "" {
			genres = []string{capTagString(m.Genre())}
		}
		trackNum, _ = m.Track()
		discNum, _ = m.Disc()
		isrc = extractISRC(m.Raw())
	}

	dur, format := readFileInfo(f, path)

	return provider.Track{
		ID:          fmt.Sprintf("local:%s", path),
		Title:       provider.Clean(capTagString(title)),
		Artist:      provider.Clean(capTagString(artist)),
		Album:       provider.Clean(capTagString(album)),
		Duration:    dur,
		Genres:      provider.CleanAll(genres),
		DiscNumber:  discNum,
		TrackNumber: trackNum,
		ISRC:        isrc,
		LocalPath:   path,
		LocalFormat: format,
	}, nil
}

func capTagString(s string) string {
	const maxTagRunes = 512
	runes := []rune(s)
	if len(runes) > maxTagRunes {
		return string(runes[:maxTagRunes])
	}
	return s
}

const (
	maxTagReadBytes    = 32 << 20 // 32 MiB total bytes read while parsing tags
	maxTagBlockBytes   = 32 << 20 // 32 MiB cap for a single FLAC metadata block
	maxTagPictureBytes = 16 << 20 // 16 MiB cap for embedded FLAC picture payload
)

type boundedReadSeeker struct {
	rs        io.ReadSeeker
	remaining int64
}

func (b *boundedReadSeeker) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("tag metadata read budget exceeded")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.rs.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *boundedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	return b.rs.Seek(offset, whence)
}

// safeReadTags wraps tag.ReadFrom with panic recovery, bounded read I/O, and
// pre-flight FLAC/OGG/Vorbis block and picture bounds validation so malformed
// tags cannot panic or trigger multi-gigabyte allocations inside dhowden/tag.
func safeReadTags(rs io.ReadSeeker) (m tag.Metadata, err error) {
	defer func() {
		if r := recover(); r != nil {
			m = nil
			err = fmt.Errorf("tag parser panic: %v", r)
		}
	}()

	fileSize, err := rs.Seek(0, io.SeekEnd)
	if err != nil || fileSize <= 0 {
		return nil, errors.New("empty or unseekable audio file")
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var magic [4]byte
	if n, _ := io.ReadFull(rs, magic[:]); n == 4 && string(magic[:]) == "OggS" {
		if _, err := rs.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := validateOGGTagsBounds(rs, fileSize); err != nil {
			return nil, err
		}
	} else {
		if _, err := rs.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := validateFLACTagBounds(rs, fileSize); err != nil {
			return nil, err
		}
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return tag.ReadFrom(&boundedReadSeeker{rs: rs, remaining: maxTagReadBytes})
}

const maxVorbisComments = 8192

func validateFLACTagBounds(rs io.ReadSeeker, fileSize int64) error {
	if err := skipID3v2(rs); err != nil {
		return nil // let tag.ReadFrom handle non-ID3/short inputs
	}
	var magic [4]byte
	if _, err := io.ReadFull(rs, magic[:]); err != nil || string(magic[:]) != "fLaC" {
		return nil
	}
	for range maxMetadataBlocks {
		var hdr [4]byte
		if _, err := io.ReadFull(rs, hdr[:]); err != nil {
			return err
		}
		last := hdr[0]&0x80 != 0
		typ := hdr[0] & 0x7F
		blockLen := int64(hdr[1])<<16 | int64(hdr[2])<<8 | int64(hdr[3])
		curPos, err := rs.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if blockLen < 0 || blockLen > fileSize-curPos || blockLen > maxTagBlockBytes {
			return errors.New("flac metadata block exceeds file or memory bounds")
		}
		switch typ {
		case 4:
			if err := validateVorbisCommentBounds(io.LimitReader(rs, blockLen), blockLen); err != nil {
				return err
			}
		case 6:
			if err := validatePictureBlockReader(io.LimitReader(rs, blockLen), blockLen); err != nil {
				return err
			}
		}
		if _, err := rs.Seek(curPos+blockLen, io.SeekStart); err != nil {
			return err
		}
		if last {
			return nil
		}
	}
	return errors.New("flac metadata block count exceeded")
}

func validateOGGTagsBounds(rs io.ReadSeeker, fileSize int64) error {
	var (
		vorbisPrefix = []byte("\x03vorbis")
		opusPrefix   = []byte("OpusTags")
	)
	packets := make(map[uint32]*bytes.Buffer)
	var readTotal int64
	for range maxMetadataBlocks {
		if readTotal >= maxTagReadBytes || readTotal >= fileSize {
			return nil
		}
		var hdr [27]byte
		if _, err := io.ReadFull(rs, hdr[:]); err != nil {
			return nil
		}
		readTotal += 27
		if string(hdr[:4]) != "OggS" {
			return errors.New("malformed ogg page header")
		}
		flags := hdr[5]
		serial := binary.LittleEndian.Uint32(hdr[14:18])
		segCount := int(hdr[26])
		segTable := make([]byte, segCount)
		if _, err := io.ReadFull(rs, segTable); err != nil {
			return nil
		}
		readTotal += int64(segCount)
		var segTotal int64
		for _, s := range segTable {
			segTotal += int64(s)
		}
		if segTotal < 0 || segTotal > fileSize-readTotal || readTotal+segTotal > maxTagReadBytes {
			return errors.New("ogg page payload exceeds file or memory bounds")
		}
		segData := make([]byte, segTotal)
		if _, err := io.ReadFull(rs, segData); err != nil {
			return nil
		}
		readTotal += segTotal

		var pktBuf *bytes.Buffer
		if flags&0x01 != 0 {
			var ok bool
			pktBuf, ok = packets[serial]
			if !ok {
				return errors.New("unexpected continued ogg packet")
			}
		} else {
			if len(packets) >= 16 {
				return errors.New("too many concurrent ogg streams")
			}
			pktBuf = &bytes.Buffer{}
		}

		var pos int
		for _, s := range segTable {
			n := int(s)
			if int64(pktBuf.Len()+n) > maxTagBlockBytes {
				return errors.New("ogg comment packet exceeds memory bounds")
			}
			pktBuf.Write(segData[pos : pos+n])
			pos += n
			if s < 255 {
				pkt := pktBuf.Bytes()
				switch {
				case bytes.HasPrefix(pkt, vorbisPrefix):
					payload := pkt[len(vorbisPrefix):]
					return validateVorbisCommentBounds(bytes.NewReader(payload), int64(len(payload)))
				case bytes.HasPrefix(pkt, opusPrefix):
					payload := pkt[len(opusPrefix):]
					return validateVorbisCommentBounds(bytes.NewReader(payload), int64(len(payload)))
				}
				pktBuf = &bytes.Buffer{}
			}
		}
		packets[serial] = pktBuf
	}
	return nil
}

func validateVorbisCommentBounds(r io.Reader, rem int64) error {
	if rem < 8 || rem > maxTagBlockBytes {
		return errors.New("vorbis comment block out of bounds")
	}
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	rem -= 4
	vendorLen := int64(binary.LittleEndian.Uint32(lenBuf[:]))
	if vendorLen < 0 || vendorLen > rem-4 || vendorLen > maxTagBlockBytes {
		return errors.New("vorbis vendor length out of bounds")
	}
	if _, err := io.CopyN(io.Discard, r, vendorLen); err != nil {
		return err
	}
	rem -= vendorLen

	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	rem -= 4
	commentsLen := binary.LittleEndian.Uint32(lenBuf[:])
	if commentsLen > maxVorbisComments || int64(commentsLen)*4 > rem {
		return errors.New("vorbis comment count out of bounds")
	}

	const picKey = "metadata_block_picture="
	for range commentsLen {
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return err
		}
		rem -= 4
		l := int64(binary.LittleEndian.Uint32(lenBuf[:]))
		if l < 0 || l > rem || l > maxTagBlockBytes {
			return errors.New("vorbis comment entry length out of bounds")
		}
		rem -= l
		if l <= int64(len(picKey)) {
			if _, err := io.CopyN(io.Discard, r, l); err != nil {
				return err
			}
			continue
		}
		var prefix [len(picKey)]byte
		if _, err := io.ReadFull(r, prefix[:]); err != nil {
			return err
		}
		restLen := l - int64(len(picKey))
		if !strings.EqualFold(string(prefix[:]), picKey) {
			if _, err := io.CopyN(io.Discard, r, restLen); err != nil {
				return err
			}
			continue
		}
		if restLen > int64(base64.StdEncoding.EncodedLen(maxTagBlockBytes)) {
			return errors.New("vorbis picture base64 exceeds memory bounds")
		}
		b64 := make([]byte, restLen)
		if _, err := io.ReadFull(r, b64); err != nil {
			return err
		}
		decoded, err := base64.StdEncoding.DecodeString(string(b64))
		if err != nil {
			return err
		}
		if err := validatePictureBlockReader(bytes.NewReader(decoded), int64(len(decoded))); err != nil {
			return err
		}
	}
	return nil
}

func validatePictureBlockReader(r io.Reader, blockRem int64) error {
	if blockRem < 32 || blockRem > maxTagBlockBytes {
		return errors.New("truncated or oversized flac picture block")
	}
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return err
	}
	rem := blockRem - 8
	mimeLen := int64(binary.BigEndian.Uint32(head[4:8]))
	if mimeLen < 0 || mimeLen > rem-24 || mimeLen > maxTagBlockBytes {
		return errors.New("flac picture mime length out of bounds")
	}
	if _, err := io.CopyN(io.Discard, r, mimeLen); err != nil {
		return err
	}
	rem -= mimeLen

	var descHdr [4]byte
	if _, err := io.ReadFull(r, descHdr[:]); err != nil {
		return err
	}
	rem -= 4
	descLen := int64(binary.BigEndian.Uint32(descHdr[:]))
	if descLen < 0 || descLen > rem-20 || descLen > maxTagBlockBytes {
		return errors.New("flac picture description length out of bounds")
	}
	if _, err := io.CopyN(io.Discard, r, descLen+16); err != nil {
		return err
	}
	rem -= descLen + 16

	var dataHdr [4]byte
	if _, err := io.ReadFull(r, dataHdr[:]); err != nil {
		return err
	}
	rem -= 4
	dataLen := int64(binary.BigEndian.Uint32(dataHdr[:]))
	if dataLen < 0 || dataLen > maxTagPictureBytes || dataLen > rem {
		return errors.New("flac picture payload length out of bounds")
	}
	return nil
}

// extractISRC inspects raw tag maps (ID3 TSRC/TRC, Vorbis/FLAC ISRC, MP4
// freeform ----:com.apple.iTunes:ISRC) and returns a normalized 12-character
// ISRC, or "" when absent.
func extractISRC(raw map[string]any) string {
	if len(raw) == 0 {
		return ""
	}
	for k, v := range raw {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk != "tsrc" && lk != "trc" && lk != "isrc" && !strings.HasSuffix(lk, ":isrc") {
			continue
		}
		if code := normalizeRawISRC(v); code != "" {
			return code
		}
	}
	return ""
}

func normalizeRawISRC(v any) string {
	switch val := v.(type) {
	case string:
		return provider.NormalizeISRC(val)
	case []string:
		for _, s := range val {
			if code := provider.NormalizeISRC(s); code != "" {
				return code
			}
		}
	case []byte:
		return provider.NormalizeISRC(string(val))
	case fmt.Stringer:
		return provider.NormalizeISRC(val.String())
	}
	return ""
}

func (p *Provider) GetLibraryTracks(_ context.Context) ([]provider.Track, error) {
	out := make([]provider.Track, len(p.tracks))
	copy(out, p.tracks)
	return out, nil
}

func (p *Provider) Search(_ context.Context, query string) (*provider.SearchResult, error) {
	q := strings.ToLower(query)
	var tracks []provider.Track
	var albums []provider.Album
	seen := map[string]bool{}

	for _, t := range p.tracks {
		if strings.Contains(strings.ToLower(t.Title), q) ||
			strings.Contains(strings.ToLower(t.Artist), q) ||
			strings.Contains(strings.ToLower(t.Album), q) {
			tracks = append(tracks, t)
			if !seen[t.Album] {
				seen[t.Album] = true
				albums = append(albums, provider.Album{
					ID:     "local-album:" + t.Album,
					Title:  t.Album,
					Artist: t.Artist,
				})
			}
		}
	}
	return &provider.SearchResult{Tracks: tracks, Albums: albums}, nil
}

func (p *Provider) GetLibraryPlaylists(_ context.Context) ([]provider.Playlist, error) {
	return nil, nil
}

func (p *Provider) GetPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

func (p *Provider) GetAlbumTracks(_ context.Context, albumID string) ([]provider.Track, error) {
	albumID = strings.TrimPrefix(albumID, "local-album:")
	var out []provider.Track
	for _, t := range p.tracks {
		if t.Album == albumID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (p *Provider) GetLibraryAlbumTracks(ctx context.Context, albumID string) ([]provider.Track, error) {
	return p.GetAlbumTracks(ctx, albumID)
}

func (p *Provider) GetCatalogPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}

// GetArtistAlbums returns nothing: local search returns no artists to open.
func (p *Provider) GetArtistAlbums(_ context.Context, _ string) ([]provider.Album, error) {
	return nil, nil
}

func (p *Provider) CreatePlaylist(_ context.Context, _ string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{}, nil
}

func (p *Provider) SetSongRating(_ context.Context, _ string, _ provider.Rating) error { return nil }
func (p *Provider) GetSongRating(_ context.Context, _ string) (provider.Rating, error) {
	return provider.RatingNone, nil
}
func (p *Provider) AddToPlaylist(_ context.Context, _, _ string) error { return nil }

func (p *Provider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	// Group tracks by genre using their metadata tags.
	// Tracks with no genre tag are skipped.
	byGenre := map[string][]provider.Track{}
	for _, t := range p.tracks {
		for _, g := range t.Genres {
			if g != "" {
				byGenre[g] = append(byGenre[g], t)
			}
		}
	}

	var groups []provider.RecommendationGroup
	for genre, tracks := range byGenre {
		if len(tracks) < 2 {
			continue
		}
		items := make([]provider.RecommendationItem, 0, len(tracks))
		for _, t := range tracks {
			items = append(items, provider.RecommendationItem{
				ID:       t.ID,
				Kind:     "track",
				Title:    t.Title,
				Subtitle: t.Artist,
			})
		}
		groups = append(groups, provider.RecommendationGroup{
			Title: genre,
			Items: items,
		})
	}
	return groups, nil
}

func (p *Provider) GetStationTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
