//go:build linux || darwin || windows

package browserless

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/player/browserless/cdm"
	"github.com/simone-vibes/vibez/internal/player/browserless/license"
	"github.com/simone-vibes/vibez/internal/player/browserless/mp4"
	"github.com/simone-vibes/vibez/internal/provider"
)

type segmentRef struct {
	uri      string
	offset   int64
	length   int64
	duration float64
}

type trackStream struct {
	trackID   string
	sessionID string
	kid       []byte
	audioCfg  mp4.AudioConfig
	baseURI   string
	segments  []segmentRef
	totalDur  time.Duration
	createdAt time.Time

	useSeq        uint64
	activeReaders int

	// raw marks an unencrypted library upload: segments are byte ranges of
	// the file itself and pass through without decryption.
	raw       bool
	rawFormat rawFormat

	seg0Mu   sync.Mutex
	seg0AAC  []byte
	released bool
}

const (
	maxHLSPlaylistBytes = 4 << 20  // 4 MiB cap on HLS media playlist
	maxInitSegmentBytes = 4 << 20  // 4 MiB cap on MP4 init segment
	maxMediaSegmentSize = 64 << 20 // 64 MiB cap on a single HLS media segment
	maxRawUploadSize    = 2 << 30  // 2 GiB ceiling on a non-seekable whole-file upload
	maxHLSSegments      = 20000    // bounded segment count per playlist (~55h at 10s/segment)
	maxRetainedStreams  = 3
	maxStreamAge        = 30 * time.Minute
	maxPooledSegBufSize = 2 << 20 // 2 MiB cap for pooled raw/AAC segment buffers
)

// streamStallTimeout bounds how long streamRawUpload waits for upstream
// response headers or the next body chunk. It is disarmed while writing to the
// local client so downstream backpressure or pausing cannot abort a healthy
// download.
var streamStallTimeout = 30 * time.Second

type cdmBatchScratch struct {
	cdm   cdm.DecryptScratch
	subs  []cdm.Subsample
	batch []cdm.BatchSample
}

// StreamServer manages local HTTP streaming for decrypted Apple Music tracks.
type StreamServer struct {
	cdmEngine  *cdm.CDM
	licClient  *license.Client
	httpClient *http.Client
	listener   net.Listener
	server     *http.Server
	port       int
	authToken  string

	rawSegPool     sync.Pool // *bytes.Buffer
	aacBufPool     sync.Pool // *[]byte
	cdmScratchPool sync.Pool // *cdmBatchScratch

	mu            sync.RWMutex
	closed        bool
	closeCtx      context.Context
	closeCancel   context.CancelFunc
	closeOnce     sync.Once
	bgWg          sync.WaitGroup
	nextStreamSeq uint64
	streams       map[string]*trackStream
	serverCert    []byte

	// onFail and onLog are set once, before the first stream is prepared.
	// The local client cannot be told why a stream stopped once the 200 has
	// gone out, so this is the only place a failure can be reported.
	onFail func(streamID string, err error)
	onLog  func(msg string)
}

// logWriter adapts onLog for http.Server.ErrorLog.
type logWriter struct{ s *StreamServer }

func (lw logWriter) Write(b []byte) (int, error) {
	if lw.s.onLog != nil {
		lw.s.onLog(strings.TrimSpace(string(b)))
	}
	return len(b), nil
}

// NewStreamServer creates and starts a local loopback HTTP server.
func NewStreamServer(cdmEngine *cdm.CDM, licClient *license.Client) (*StreamServer, error) {
	if licClient == nil {
		return nil, errors.New("license client is required")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to bind loopback stream listener: %w", err)
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("failed to generate stream auth token: %w", err)
	}
	authToken := hex.EncodeToString(tokenBytes)

	//nolint:gosec // G118: closeCancel is stored on StreamServer and invoked in Close()
	closeCtx, closeCancel := context.WithCancel(context.Background())
	port := ln.Addr().(*net.TCPAddr).Port
	s := &StreamServer{
		cdmEngine:   cdmEngine,
		licClient:   licClient,
		httpClient:  licClient.HTTPClient(),
		listener:    ln,
		port:        port,
		authToken:   authToken,
		closeCtx:    closeCtx,
		closeCancel: closeCancel,
		streams:     make(map[string]*trackStream),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", s.handleStream)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Route net/http's own errors to the debug log: stderr would corrupt the TUI.
		ErrorLog: log.New(logWriter{s}, "browserless stream: ", 0),
	}

	go func() {
		_ = s.server.Serve(ln)
	}()

	return s, nil
}

var defaultUpstreamClient = license.NewClient(nil).HTTPClient()

func (s *StreamServer) upstreamClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	if s.licClient != nil {
		return s.licClient.HTTPClient()
	}
	return defaultUpstreamClient
}

func resolveUpstreamURL(baseRaw, refRaw string) (string, error) {
	if refRaw == "" || strings.ContainsAny(refRaw, "\r\n\x00") {
		return "", errors.New("empty or control-character URI")
	}
	refU, err := url.Parse(refRaw)
	if err != nil {
		return "", fmt.Errorf("invalid URI: %w", err)
	}
	if refU.User != nil {
		return "", errors.New("URI must not contain userinfo")
	}
	resolved := refU
	if !refU.IsAbs() {
		if baseRaw == "" {
			return "", errors.New("relative URI without base URL")
		}
		baseU, err := url.Parse(baseRaw)
		if err != nil {
			return "", fmt.Errorf("invalid base URL: %w", err)
		}
		resolved = baseU.ResolveReference(refU)
	}
	resolvedStr := resolved.String()
	if err := license.ValidateUpstreamURL(resolvedStr); err != nil {
		return "", err
	}
	return resolvedStr, nil
}

func parseByteRangeAttr(spec string, hasDefaultOffset bool, defaultOffset int64) (length, offset int64, ok bool) {
	var err error
	if lStr, oStr, hasAt := strings.Cut(spec, "@"); hasAt {
		length, err = strconv.ParseInt(strings.TrimSpace(lStr), 10, 64)
		if err != nil || length <= 0 || length > maxMediaSegmentSize {
			return 0, 0, false
		}
		offset, err = strconv.ParseInt(strings.TrimSpace(oStr), 10, 64)
		if err != nil || offset < 0 || offset > math.MaxInt64-length {
			return 0, 0, false
		}
		return length, offset, true
	}
	length, err = strconv.ParseInt(strings.TrimSpace(spec), 10, 64)
	if err != nil || length <= 0 || length > maxMediaSegmentSize {
		return 0, 0, false
	}
	if !hasDefaultOffset {
		defaultOffset = 0
	}
	if defaultOffset < 0 || defaultOffset > math.MaxInt64-length {
		return 0, 0, false
	}
	return length, defaultOffset, true
}

// PrepareTrack resolves Apple Music playback metadata, acquires the Widevine license,
// parses the HLS manifest, and returns a local HTTP streaming URL and total duration.
func (s *StreamServer) PrepareTrack(ctx context.Context, trackID string, prefer256k bool) (string, time.Duration, error) {
	if trackID == "" || strings.ContainsAny(trackID, "/\\?#\r\n\x00") {
		return "", 0, fmt.Errorf("invalid track ID %q", provider.Clean(trackID))
	}
	// A library ID reaches here only when it has no catalog song.
	if strings.HasPrefix(trackID, "i.") {
		return s.prepareRawTrack(ctx, trackID)
	}

	// 1. Fetch playback info via MZPlay
	info, err := s.licClient.FetchPlaybackInfo(ctx, trackID, prefer256k)
	if err != nil {
		return "", 0, fmt.Errorf("fetch playback info: %w", err)
	}

	// 2. Fetch server cert if not cached
	s.mu.Lock()
	if s.serverCert == nil {
		cert, err := s.licClient.FetchServerCertificate(ctx, info.WidevineCertURL)
		if err != nil {
			s.mu.Unlock()
			return "", 0, fmt.Errorf("fetch server cert: %w", err)
		}
		if err := s.cdmEngine.SetServerCertificate(cert); err != nil {
			s.mu.Unlock()
			return "", 0, fmt.Errorf("set server cert: %w", err)
		}
		s.serverCert = cert
	}
	s.mu.Unlock()

	// 3. Download HLS playlist
	hlsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, info.HLSPlaylistURL, nil)
	if err != nil {
		return "", 0, err
	}
	hlsResp, err := s.upstreamClient().Do(hlsReq)
	if err != nil {
		return "", 0, fmt.Errorf("fetch playlist: %w", err)
	}
	defer func() { _ = hlsResp.Body.Close() }()

	if hlsResp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("fetch playlist status %d: %s", hlsResp.StatusCode, hlsResp.Status)
	}

	playlistBytes, err := io.ReadAll(io.LimitReader(hlsResp.Body, maxHLSPlaylistBytes+1))
	if err != nil {
		return "", 0, fmt.Errorf("read playlist: %w", err)
	}
	if len(playlistBytes) > maxHLSPlaylistBytes {
		return "", 0, fmt.Errorf("playlist exceeds maximum size (%d bytes)", maxHLSPlaylistBytes)
	}

	// 4. Parse HLS playlist
	parsed, err := parseHLSPlaylist(playlistBytes)
	if err != nil {
		return "", 0, err
	}
	keyURI := parsed.keyURI
	initURI := parsed.initURI
	initOffset, initLength := parsed.initOffset, parsed.initLength
	segments := parsed.segments

	if keyURI == "" {
		return "", 0, fmt.Errorf("no key URI found in playlist")
	}
	b64KID, ok := strings.CutPrefix(keyURI, "data:;base64,")
	if !ok {
		return "", 0, fmt.Errorf("unsupported key URI format")
	}
	kidBytes, err := base64.StdEncoding.DecodeString(b64KID)
	if err != nil || len(kidBytes) != 16 {
		return "", 0, fmt.Errorf("invalid KID (%s): %w", provider.Clean(keyURI), err)
	}

	// 5. Generate CDM challenge & acquire license
	challenge, sessionID, err := s.cdmEngine.GenerateChallenge(kidBytes)
	if err != nil {
		return "", 0, fmt.Errorf("generate challenge: %w", err)
	}
	registered := false
	defer func() {
		if !registered && sessionID != "" && s.cdmEngine != nil {
			_ = s.cdmEngine.CloseSession(sessionID)
		}
	}()
	licBytes, err := s.licClient.AcquireLicense(ctx, info.HLSKeyServerURL, challenge, keyURI, trackID)
	if err != nil {
		return "", 0, fmt.Errorf("acquire license: %w", err)
	}
	if err := s.cdmEngine.UpdateSession(sessionID, licBytes); err != nil {
		return "", 0, fmt.Errorf("update session: %w", err)
	}

	// 6. Base URI & Init segment
	baseURI := info.HLSPlaylistURL
	if err := license.ValidateUpstreamURL(baseURI); err != nil {
		return "", 0, fmt.Errorf("invalid playlist base URL: %w", err)
	}
	if lastSlash := strings.LastIndex(baseURI, "/"); lastSlash != -1 {
		baseURI = baseURI[:lastSlash+1]
	}

	var initBytes []byte
	if initURI != "" {
		fullInitURL, err := resolveUpstreamURL(baseURI, initURI)
		if err != nil {
			return "", 0, fmt.Errorf("invalid init segment URL: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullInitURL, nil)
		if err == nil {
			if initLength > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", initOffset, initOffset+initLength-1))
			}
			if resp, err := s.upstreamClient().Do(req); err == nil {
				if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
					initBytes, _ = io.ReadAll(io.LimitReader(resp.Body, maxInitSegmentBytes))
				}
				_ = resp.Body.Close()
			}
		}
	}
	for i := range segments {
		resolvedSeg, err := resolveUpstreamURL(baseURI, segments[i].uri)
		if err != nil {
			return "", 0, fmt.Errorf("invalid segment %d URL: %w", i, err)
		}
		segments[i].uri = resolvedSeg
	}
	audioCfg, _ := mp4.ParseAudioConfig(initBytes)

	// Calculate total duration
	var totalSeconds float64
	for _, seg := range segments {
		totalSeconds += seg.duration
	}
	totalDur := time.Duration(totalSeconds * float64(time.Second))

	streamID := fmt.Sprintf("%s-%d", trackID, time.Now().UnixNano())
	st := &trackStream{
		trackID:   trackID,
		sessionID: sessionID,
		kid:       kidBytes,
		audioCfg:  audioCfg,
		baseURI:   baseURI,
		segments:  segments,
		totalDur:  totalDur,
		createdAt: time.Now(),
	}

	registered = true
	return s.register(ctx, streamID, st, ".aac"), totalDur, nil
}

// rawSegmentBytes is the size of each byte range an upload is split into.
// About ten seconds of a 192 kbps MP3, like an HLS segment.
const rawSegmentBytes = 256 << 10

// prepareRawTrack serves a library upload, which Apple hands out as a plain
// file with no DRM. The file is split into byte-range segments so retries,
// segment 0 warming and start_seg seeking work as they do for HLS.
func (s *StreamServer) prepareRawTrack(ctx context.Context, libraryID string) (string, time.Duration, error) {
	assetURL, err := s.licClient.FetchLibraryAssetURL(ctx, libraryID)
	if err != nil {
		return "", 0, fmt.Errorf("fetch playback info: %w", err)
	}
	if err := license.ValidateUpstreamURL(assetURL); err != nil {
		return "", 0, fmt.Errorf("invalid library asset URL: %w", err)
	}

	head, size, err := fetchRawHeadWithClient(ctx, s.upstreamClient(), assetURL)
	if err != nil {
		return "", 0, fmt.Errorf("fetch upload: %w", err)
	}
	if size > maxRawUploadSize {
		return "", 0, fmt.Errorf("upload exceeds maximum size (%d bytes)", maxRawUploadSize)
	}
	format, ok := sniffRawFormat(head, size)
	if !ok {
		return "", 0, fmt.Errorf("%w: unsupported upload format (library item %s)", license.ErrTrackUnavailable, provider.Clean(libraryID))
	}

	var segments []segmentRef
	if !format.seekable {
		// One segment: an MP4 cannot be cut, and its whole file goes out as is.
		segments = []segmentRef{{uri: assetURL, offset: 0, length: size}}
	} else {
		for off := int64(0); off < size; off += rawSegmentBytes {
			n := min(int64(rawSegmentBytes), size-off)
			segments = append(segments, segmentRef{
				uri:      assetURL,
				offset:   off,
				length:   n,
				duration: format.duration.Seconds() * float64(n) / float64(size),
			})
		}
	}

	streamID := fmt.Sprintf("%s-%d", libraryID, time.Now().UnixNano())
	st := &trackStream{
		trackID:   libraryID,
		segments:  segments,
		totalDur:  format.duration,
		createdAt: time.Now(),
		raw:       true,
		rawFormat: format,
	}
	return s.register(ctx, streamID, st, format.ext), format.duration, nil
}

// fetchRawHead reads the first 64 KiB of an upload, enough to identify it,
// and its total size.
func fetchRawHead(ctx context.Context, assetURL string) ([]byte, int64, error) {
	return fetchRawHeadWithClient(ctx, defaultUpstreamClient, assetURL)
}

func fetchRawHeadWithClient(ctx context.Context, client *http.Client, assetURL string) ([]byte, int64, error) {
	if err := license.ValidateUpstreamURL(assetURL); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Range", "bytes=0-65535")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	var size int64
	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Content-Range: bytes 0-65535/<total>
		if _, total, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
			size, err = strconv.ParseInt(strings.TrimSpace(total), 10, 64)
			if err != nil {
				size = 0
			}
		}
	case http.StatusOK:
		size = resp.ContentLength
	default:
		return nil, 0, fmt.Errorf("status %s", resp.Status)
	}
	if size <= 0 || size > maxRawUploadSize {
		return nil, 0, fmt.Errorf("invalid upload size: %d", size)
	}
	head, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return nil, 0, err
	}
	return head, size, nil
}

type evictedStream struct {
	stream    *trackStream
	sessionID string
}

func (s *StreamServer) touchStreamLocked(st *trackStream) {
	s.nextStreamSeq++
	st.useSeq = s.nextStreamSeq
}

// collectEvictableStreamsLocked prunes expired idle streams and trims excess
// idle streams beyond maxRetainedStreams in LRU order. Streams with active
// HTTP readers are never evicted. Caller must hold s.mu.
func (s *StreamServer) collectEvictableStreamsLocked(now time.Time) []evictedStream {
	var evicted []evictedStream

	type idleEntry struct {
		id string
		st *trackStream
	}
	var idleEntries []idleEntry
	for id, stream := range s.streams {
		if stream.activeReaders > 0 {
			continue
		}
		if !stream.createdAt.IsZero() && now.Sub(stream.createdAt) > maxStreamAge {
			delete(s.streams, id)
			evicted = append(evicted, evictedStream{stream: stream, sessionID: stream.sessionID})
			stream.sessionID = ""
			continue
		}
		idleEntries = append(idleEntries, idleEntry{id: id, st: stream})
	}

	excess := len(s.streams) - maxRetainedStreams
	if excess <= 0 || len(idleEntries) == 0 {
		return evicted
	}

	slices.SortFunc(idleEntries, func(a, b idleEntry) int {
		if c := cmp.Compare(a.st.useSeq, b.st.useSeq); c != 0 {
			return c
		}
		return a.st.createdAt.Compare(b.st.createdAt)
	})
	for i := 0; i < len(idleEntries) && excess > 0; i++ {
		entry := idleEntries[i]
		delete(s.streams, entry.id)
		evicted = append(evicted, evictedStream{stream: entry.st, sessionID: entry.st.sessionID})
		entry.st.sessionID = ""
		excess--
	}
	return evicted
}

func (s *StreamServer) releaseStreams(evicted []evictedStream) {
	for _, ev := range evicted {
		if ev.stream != nil {
			ev.stream.seg0Mu.Lock()
			ev.stream.released = true
			ev.stream.seg0AAC = nil
			ev.stream.seg0Mu.Unlock()
		}
		if ev.sessionID != "" && s.cdmEngine != nil {
			_ = s.cdmEngine.CloseSession(ev.sessionID)
		}
	}
}

// register stores a prepared stream and returns its loopback URL. The sinks
// apply the AAC priming trim only to .aac streams.
func (s *StreamServer) register(ctx context.Context, streamID string, st *trackStream, ext string) string {
	s.mu.Lock()
	if s.closed {
		sessionID := st.sessionID
		st.sessionID = ""
		s.mu.Unlock()
		if sessionID != "" && s.cdmEngine != nil {
			_ = s.cdmEngine.CloseSession(sessionID)
		}
		return ""
	}
	if s.streams == nil {
		s.streams = make(map[string]*trackStream)
	}
	s.touchStreamLocked(st)
	s.streams[streamID] = st
	evicted := s.collectEvictableStreamsLocked(time.Now())
	shouldWarm := !s.closed && len(st.segments) > 0 &&
		(!st.raw || st.rawFormat.seekable) &&
		st.segments[0].length <= maxMediaSegmentSize &&
		(st.raw || s.cdmEngine != nil)
	baseCtx := ctx
	if s.closeCtx != nil {
		baseCtx = s.closeCtx
	}
	if shouldWarm {
		st.activeReaders++
		s.bgWg.Add(1)
	}
	s.mu.Unlock()
	s.releaseStreams(evicted)

	// Warm segment 0 asynchronously so both initial playback and
	// seek-to-start (0:00) can write the first ~10s immediately
	// without waiting for an upstream CDN round-trip.
	if shouldWarm {
		go func() {
			defer func() {
				s.mu.Lock()
				st.activeReaders--
				ev := s.collectEvictableStreamsLocked(time.Now())
				s.mu.Unlock()
				s.releaseStreams(ev)
				s.bgWg.Done()
			}()
			if ctx.Err() != nil {
				return
			}
			warmCtx, cancel := context.WithTimeout(baseCtx, 10*time.Second)
			defer cancel()
			stopAfter := context.AfterFunc(ctx, cancel)
			defer stopAfter()
			_, _, _ = s.getOrFetchSegment0(warmCtx, s.upstreamClient(), st)
		}()
	}

	return fmt.Sprintf("http://127.0.0.1:%d/stream/%s/%s%s", s.port, streamID, s.authToken, ext)
}

// streamIDFromURL returns the stream ID a PrepareTrack URL carries.
func streamIDFromURL(streamURL string) string {
	_, rest, _ := strings.Cut(streamURL, "/stream/")
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// SeekTarget resolves a target playback timestamp into the starting HLS segment
// index and that segment's exact starting time offset within the track.
func (s *StreamServer) SeekTarget(streamID string, target time.Duration) (int, time.Duration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, ok := s.streams[streamID]
	if !ok || len(st.segments) == 0 {
		return 0, 0, false
	}
	if st.raw && !st.rawFormat.seekable && target > 0 {
		return 0, 0, false
	}
	if target <= 0 {
		return 0, 0, true
	}
	targetSec := target.Seconds()
	var acc float64
	for i, seg := range st.segments {
		if acc+seg.duration > targetSec || i == len(st.segments)-1 {
			return i, time.Duration(acc * float64(time.Second)), true
		}
		acc += seg.duration
	}
	return 0, 0, true
}

func (s *StreamServer) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	// Verify request is from loopback interface
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Verify Host header points to local loopback
	reqHost := r.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	if reqHost != "127.0.0.1" && reqHost != "localhost" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Path format: /stream/{streamID}/{token}.{aac,mp3,m4a}
	path := strings.TrimPrefix(r.URL.Path, "/stream/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	streamID := parts[0]
	filePart := parts[1]
	token, _, _ := strings.Cut(filePart, ".")
	if s.authToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	st, ok := s.streams[streamID]
	if !ok {
		s.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	st.activeReaders++
	s.touchStreamLocked(st)
	closeCtx := s.closeCtx
	s.bgWg.Add(1)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		st.activeReaders--
		s.touchStreamLocked(st)
		evicted := s.collectEvictableStreamsLocked(time.Now())
		s.mu.Unlock()
		s.releaseStreams(evicted)
		s.bgWg.Done()
	}()

	// Support start_seg parameter for instant seeking
	startSeg := 0
	if qSeg := r.URL.Query().Get("start_seg"); qSeg != "" {
		if val, err := strconv.Atoi(qSeg); err == nil && val >= 0 && val < len(st.segments) {
			startSeg = val
		}
	} else if qTime := r.URL.Query().Get("t"); qTime != "" {
		if sec, err := strconv.ParseFloat(qTime, 64); err == nil && !math.IsNaN(sec) && !math.IsInf(sec, 0) && sec > 0 {
			var acc float64
			for i, seg := range st.segments {
				if acc+seg.duration > sec {
					startSeg = i
					break
				}
				acc += seg.duration
			}
		}
	}

	contentType := "audio/aac"
	if st.raw {
		contentType = st.rawFormat.contentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	flusher, _ := w.(http.Flusher)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if closeCtx != nil {
		stopOnServerClose := context.AfterFunc(closeCtx, cancel)
		defer stopOnServerClose()
	}
	client := s.upstreamClient()

	// fail reports why the stream stopped early, then aborts the response so
	// the client sees a truncated body rather than a clean end of stream. A
	// cancelled request is the client going away on skip or stop, not a
	// failure, and is not reported.
	fail := func(seg int, stage string, err error) {
		if ctx.Err() != nil {
			return
		}
		if s.onFail != nil {
			s.onFail(streamID, fmt.Errorf("stream %s: segment %d/%d: %s: %w", st.trackID, seg+1, len(st.segments), stage, err))
		}
		panic(http.ErrAbortHandler)
	}

	// Stream non-seekable raw uploads (e.g. large M4A mixes) chunk-by-chunk
	// instead of buffering up to maxRawUploadSize on heap.
	if st.raw && !st.rawFormat.seekable && len(st.segments) == 1 && startSeg == 0 {
		if stage, err := s.streamRawUpload(ctx, client, st, st.segments[0], w, flusher); err != nil {
			if ctx.Err() != nil {
				return
			}
			fail(0, stage, err)
		}
		return
	}

	type segResult struct {
		aacData   []byte
		pooledAAC *[]byte
		stage     string
		err       error
	}

	lookCtx, cancelLook := context.WithCancel(ctx)
	var lookWg sync.WaitGroup
	var nextCh <-chan segResult
	defer func() {
		cancelLook()
		lookWg.Wait()
		if nextCh != nil {
			select {
			case r := <-nextCh:
				s.putAACBuf(r.pooledAAC)
			default:
			}
		}
	}()

	fetchSeg := func(fetchCtx context.Context, idx int) segResult {
		if idx == 0 {
			data, stage, err := s.getOrFetchSegment0(fetchCtx, client, st)
			return segResult{aacData: data, stage: stage, err: err}
		}
		if st.raw {
			data, stage, err := s.fetchAndDecryptSegmentInto(fetchCtx, client, st, st.segments[idx], nil)
			return segResult{aacData: data, stage: stage, err: err}
		}
		bufPtr := s.getAACBuf()
		data, stage, err := s.fetchAndDecryptSegmentInto(fetchCtx, client, st, st.segments[idx], (*bufPtr)[:0])
		if err != nil {
			s.putAACBuf(bufPtr)
			return segResult{stage: stage, err: err}
		}
		*bufPtr = data
		return segResult{aacData: data, pooledAAC: bufPtr, stage: stage}
	}

	for i := startSeg; i < len(st.segments); i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var res segResult
		if nextCh != nil {
			res = <-nextCh
			nextCh = nil
		} else {
			res = fetchSeg(ctx, i)
		}
		if res.err != nil {
			s.putAACBuf(res.pooledAAC)
			if ctx.Err() != nil {
				return
			}
			fail(i, res.stage, res.err)
			return
		}

		if i+1 < len(st.segments) && !st.raw {
			nextIdx := i + 1
			ch := make(chan segResult, 1)
			lookWg.Go(func() {
				ch <- fetchSeg(lookCtx, nextIdx)
			})
			nextCh = ch
		}

		// Write to HTTP client (GStreamer). A failed write means the client
		// has gone, so there is nobody left to abort for; log it only.
		_, writeErr := w.Write(res.aacData)
		s.putAACBuf(res.pooledAAC)
		res.aacData = nil
		res.pooledAAC = nil
		if writeErr != nil {
			if ctx.Err() == nil && s.onLog != nil {
				s.onLog(fmt.Sprintf("browserless stream: %s: segment %d/%d: write: %v", st.trackID, i+1, len(st.segments), writeErr))
			}
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (s *StreamServer) getOrFetchSegment0(ctx context.Context, client *http.Client, st *trackStream) ([]byte, string, error) {
	st.seg0Mu.Lock()
	defer st.seg0Mu.Unlock()
	if len(st.seg0AAC) > 0 {
		return st.seg0AAC, "", nil
	}
	if st.released || len(st.segments) == 0 {
		return nil, "fetch", errors.New("stream released")
	}
	aacData, stage, err := s.fetchAndDecryptSegment(ctx, client, st, st.segments[0])
	if err != nil {
		return nil, stage, err
	}
	if st.released {
		return nil, "fetch", errors.New("stream released")
	}
	st.seg0AAC = aacData
	return aacData, "", nil
}

func (s *StreamServer) streamRawUpload(
	ctx context.Context,
	client *http.Client,
	st *trackStream,
	seg segmentRef,
	w http.ResponseWriter,
	flusher http.Flusher,
) (string, error) {
	segURL, err := resolveUpstreamURL(st.baseURI, seg.uri)
	if err != nil {
		return "build request", err
	}

	maxBytes := int64(maxRawUploadSize)
	if seg.length > 0 {
		maxBytes = min(seg.length, int64(maxRawUploadSize))
	}

	// Keep an overall client free of total-duration cutoff on long mixes, while
	// bounding response headers and inter-chunk reads with streamStallTimeout.
	streamClient := client
	if client != nil && client.Timeout > 0 {
		cCopy := *client
		cCopy.Timeout = 0
		streamClient = &cCopy
	}

	var lastStage string
	var lastErr error
	var buf [32 << 10]byte

	for attempt := 0; attempt <= len(segmentRetryBackoffs); attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(segmentRetryBackoffs[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return lastStage, ctx.Err()
			case <-timer.C:
			}
		}

		reqCtx, cancelReq := context.WithCancel(ctx)
		stallTimer := time.AfterFunc(streamStallTimeout, cancelReq)

		req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodGet, segURL, nil)
		if reqErr != nil {
			stallTimer.Stop()
			cancelReq()
			return "build request", reqErr
		}
		if seg.length > 0 && seg.offset >= 0 && seg.offset <= math.MaxInt64-seg.length {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", seg.offset, seg.offset+seg.length-1))
		}

		resp, doErr := streamClient.Do(req)
		stallTimer.Stop()
		if doErr != nil {
			cancelReq()
			lastStage = "fetch"
			lastErr = doErr
			if ctx.Err() != nil {
				return lastStage, lastErr
			}
			continue
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			_ = resp.Body.Close()
			cancelReq()
			lastStage = "fetch"
			lastErr = fmt.Errorf("status %s", resp.Status)
			if resp.StatusCode != http.StatusRequestTimeout &&
				resp.StatusCode != http.StatusTooManyRequests &&
				resp.StatusCode < http.StatusInternalServerError {
				return lastStage, lastErr
			}
			continue
		}

		var totalWritten int64
		var writeErr error
		var readErr error
		lr := io.LimitReader(resp.Body, maxBytes+1)
		for {
			stallTimer.Reset(streamStallTimeout)
			n, rErr := lr.Read(buf[:])
			stallTimer.Stop()
			if n > 0 {
				totalWritten += int64(n)
				if totalWritten > maxBytes {
					readErr = fmt.Errorf("segment exceeds maximum size (%d bytes)", maxBytes)
					break
				}
				if _, wErr := w.Write(buf[:n]); wErr != nil {
					writeErr = wErr
					break
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if rErr != nil {
				if rErr != io.EOF {
					readErr = rErr
				}
				break
			}
		}

		_ = resp.Body.Close()
		stallTimer.Stop()
		cancelReq()

		if writeErr != nil {
			if ctx.Err() == nil && s.onLog != nil {
				s.onLog(fmt.Sprintf("browserless stream: %s: segment 1/1: write: %v", st.trackID, writeErr))
			}
			return "", nil
		}
		if readErr != nil {
			if totalWritten == 0 && ctx.Err() == nil {
				lastStage = "read"
				lastErr = readErr
				continue
			}
			return "read", readErr
		}
		if totalWritten == 0 || (seg.length > 0 && totalWritten != seg.length) {
			return "read", io.ErrUnexpectedEOF
		}
		return "", nil
	}

	return lastStage, lastErr
}

func (s *StreamServer) getRawSegBuf() *bytes.Buffer {
	if v := s.rawSegPool.Get(); v != nil {
		b := v.(*bytes.Buffer)
		b.Reset()
		return b
	}
	return &bytes.Buffer{}
}

func (s *StreamServer) putRawSegBuf(b *bytes.Buffer) {
	if b == nil || b.Cap() > maxPooledSegBufSize {
		return
	}
	b.Reset()
	s.rawSegPool.Put(b)
}

func (s *StreamServer) getAACBuf() *[]byte {
	if v := s.aacBufPool.Get(); v != nil {
		p := v.(*[]byte)
		*p = (*p)[:0]
		return p
	}
	b := make([]byte, 0, 256<<10)
	return &b
}

func (s *StreamServer) putAACBuf(p *[]byte) {
	if p == nil || cap(*p) > maxPooledSegBufSize {
		return
	}
	*p = (*p)[:0]
	s.aacBufPool.Put(p)
}

func (s *StreamServer) getCDMBatchScratch() *cdmBatchScratch {
	if v := s.cdmScratchPool.Get(); v != nil {
		return v.(*cdmBatchScratch)
	}
	return &cdmBatchScratch{}
}

func (s *StreamServer) putCDMBatchScratch(sc *cdmBatchScratch) {
	if sc == nil {
		return
	}
	clear(sc.batch)
	if cap(sc.batch) > 16384 {
		sc.batch = nil
	} else {
		sc.batch = sc.batch[:0]
	}
	if cap(sc.subs) > 32768 {
		sc.subs = nil
	} else {
		sc.subs = sc.subs[:0]
	}
	s.cdmScratchPool.Put(sc)
}

func (s *StreamServer) fetchAndDecryptSegment(ctx context.Context, client *http.Client, st *trackStream, seg segmentRef) ([]byte, string, error) {
	return s.fetchAndDecryptSegmentInto(ctx, client, st, seg, nil)
}

func (s *StreamServer) fetchAndDecryptSegmentInto(ctx context.Context, client *http.Client, st *trackStream, seg segmentRef, dstAAC []byte) ([]byte, string, error) {
	segURL, err := resolveUpstreamURL(st.baseURI, seg.uri)
	if err != nil {
		return nil, "build request", err
	}

	maxBytes := int64(maxMediaSegmentSize)
	if st.raw && seg.length > 0 {
		maxBytes = min(seg.length, int64(maxMediaSegmentSize))
	}

	if st.raw {
		segData, stage, err := fetchSegmentWithRetryMax(ctx, client, segURL, seg.offset, seg.length, maxBytes)
		if err != nil {
			return nil, stage, err
		}
		return segData, "", nil
	}

	rawBuf := s.getRawSegBuf()
	defer s.putRawSegBuf(rawBuf)

	segData, stage, err := fetchSegmentWithRetryInto(ctx, client, segURL, seg.offset, seg.length, maxBytes, rawBuf)
	if err != nil {
		return nil, stage, err
	}

	sc := s.getCDMBatchScratch()
	defer s.putCDMBatchScratch(sc)

	aacData, err := mp4.DecryptSegmentBatchInto(dstAAC, segData, st.kid, st.audioCfg, func(kid []byte, samples []mp4.EncryptedSample) ([][]byte, error) {
		var totalSubs int
		for _, sample := range samples {
			totalSubs += len(sample.Subsamples)
		}
		if cap(sc.subs) < totalSubs {
			sc.subs = make([]cdm.Subsample, 0, totalSubs)
		} else {
			sc.subs = sc.subs[:0]
		}
		if cap(sc.batch) < len(samples) {
			sc.batch = make([]cdm.BatchSample, len(samples))
		} else {
			sc.batch = sc.batch[:len(samples)]
		}
		for i, sample := range samples {
			subStart := len(sc.subs)
			for _, sb := range sample.Subsamples {
				sc.subs = append(sc.subs, cdm.Subsample{
					ClearBytes:  sb.ClearBytes,
					CipherBytes: sb.CipherBytes,
				})
			}
			var subs []cdm.Subsample
			if len(sample.Subsamples) > 0 {
				subs = sc.subs[subStart : subStart+len(sample.Subsamples)]
			}
			sc.batch[i] = cdm.BatchSample{
				IV:         sample.IV,
				Input:      sample.Data,
				Subsamples: subs,
			}
		}
		return s.cdmEngine.DecryptBatch(&sc.cdm, kid, sc.batch)
	})
	if err != nil {
		return nil, "decrypt", err
	}
	return aacData, "", nil
}

// segmentRetryBackoffs defines the wait intervals between retry attempts for
// transient CDN network or 5xx/429 errors when fetching an HLS segment.
var segmentRetryBackoffs = []time.Duration{
	200 * time.Millisecond,
	500 * time.Millisecond,
}

// fetchSegmentWithRetry downloads one HLS byte-range or segment URL bounded by
// maxMediaSegmentSize, retrying transient network/read errors and HTTP 429/5xx
// responses before giving up.
func fetchSegmentWithRetry(ctx context.Context, client *http.Client, segURL string, offset, length int64) ([]byte, string, error) {
	return fetchSegmentWithRetryMax(ctx, client, segURL, offset, length, maxMediaSegmentSize)
}

func fetchSegmentWithRetryMax(ctx context.Context, client *http.Client, segURL string, offset, length, maxBytes int64) ([]byte, string, error) {
	return fetchSegmentWithRetryInto(ctx, client, segURL, offset, length, maxBytes, nil)
}

func fetchSegmentWithRetryInto(ctx context.Context, client *http.Client, segURL string, offset, length, maxBytes int64, dstBuf *bytes.Buffer) ([]byte, string, error) {
	if maxBytes <= 0 {
		maxBytes = maxMediaSegmentSize
	}
	var lastStage string
	var lastErr error

	for attempt := 0; attempt <= len(segmentRetryBackoffs); attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(segmentRetryBackoffs[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, lastStage, ctx.Err()
			case <-timer.C:
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
		if err != nil {
			return nil, "build request", err
		}
		if length > 0 {
			if offset < 0 || offset > math.MaxInt64-length {
				return nil, "build request", errors.New("byte range overflows int64")
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
		}

		resp, err := client.Do(req)
		if err != nil {
			lastStage = "fetch"
			lastErr = err
			if ctx.Err() != nil {
				return nil, lastStage, lastErr
			}
			continue
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			_ = resp.Body.Close()
			lastStage = "fetch"
			lastErr = fmt.Errorf("status %s", resp.Status)
			// Only retry 408 Request Timeout, 429 Too Many Requests, or 5xx server errors.
			if resp.StatusCode != http.StatusRequestTimeout &&
				resp.StatusCode != http.StatusTooManyRequests &&
				resp.StatusCode < http.StatusInternalServerError {
				return nil, lastStage, lastErr
			}
			continue
		}

		segData, err := readBoundedBodyInto(resp.Body, max(resp.ContentLength, length), maxBytes, dstBuf)
		_ = resp.Body.Close()
		if err != nil {
			lastStage = "read"
			lastErr = err
			if ctx.Err() != nil {
				return nil, lastStage, lastErr
			}
			continue
		}
		if int64(len(segData)) > maxBytes {
			return nil, "read", fmt.Errorf("segment exceeds maximum size (%d bytes)", maxBytes)
		}
		return segData, "", nil
	}

	return nil, lastStage, lastErr
}

func readBoundedBodyInto(r io.Reader, sizeHint, maxBytes int64, dstBuf *bytes.Buffer) ([]byte, error) {
	hint := min(max(sizeHint, 0), min(maxBytes, maxMediaSegmentSize))
	buf := dstBuf
	if buf == nil {
		buf = &bytes.Buffer{}
	} else {
		buf.Reset()
	}
	if hint > 0 && buf.Cap() < int(hint) {
		buf.Grow(int(hint))
	}
	_, err := buf.ReadFrom(io.LimitReader(r, maxBytes+1))
	return buf.Bytes(), err
}

// Close terminates the local HTTP stream server and waits for active stream
// and prefetch handlers to exit before returning.
func (s *StreamServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		if s.closeCancel != nil {
			s.closeCancel()
		}
		var evicted []evictedStream
		if len(s.streams) > 0 {
			evicted = make([]evictedStream, 0, len(s.streams))
			for _, st := range s.streams {
				evicted = append(evicted, evictedStream{stream: st, sessionID: st.sessionID})
				st.sessionID = ""
			}
			clear(s.streams)
		}
		s.mu.Unlock()
		if s.server != nil {
			err = s.server.Close()
		}
		s.bgWg.Wait()
		s.releaseStreams(evicted)
	})
	return err
}

type hlsPlaylist struct {
	keyURI     string
	initURI    string
	initOffset int64
	initLength int64
	segments   []segmentRef
}

func nextPlaylistLine(rest []byte) ([]byte, []byte, bool) {
	if len(rest) == 0 {
		return nil, nil, false
	}
	line, tail, found := bytes.Cut(rest, []byte{'\n'})
	if !found {
		return bytes.TrimSpace(line), nil, true
	}
	return bytes.TrimSpace(line), tail, true
}

func parseHLSPlaylist(playlistBytes []byte) (hlsPlaylist, error) {
	var parsed hlsPlaylist
	var nextByteRangeEnd int64
	var currDuration float64

	rest := playlistBytes
	for {
		lineBytes, tail, ok := nextPlaylistLine(rest)
		if !ok {
			break
		}
		rest = tail
		if len(lineBytes) == 0 {
			continue
		}
		switch {
		case bytes.HasPrefix(lineBytes, []byte("#EXT-X-KEY:")):
			line := string(lineBytes)
			if _, after, ok := strings.Cut(line, "URI=\""); ok {
				if uri, _, ok := strings.Cut(after, "\""); ok {
					parsed.keyURI = uri
				}
			}
		case bytes.HasPrefix(lineBytes, []byte("#EXT-X-MAP:")):
			line := string(lineBytes)
			if _, after, ok := strings.Cut(line, "URI=\""); ok {
				if uri, _, ok := strings.Cut(after, "\""); ok {
					parsed.initURI = uri
				}
			}
			if _, after, ok := strings.Cut(line, "BYTERANGE=\""); ok {
				if rangeStr, _, ok := strings.Cut(after, "\""); ok {
					l, o, valid := parseByteRangeAttr(rangeStr, false, 0)
					if !valid {
						return hlsPlaylist{}, fmt.Errorf("invalid #EXT-X-MAP byte range %q", provider.Clean(rangeStr))
					}
					parsed.initLength = l
					parsed.initOffset = o
					nextByteRangeEnd = parsed.initOffset + parsed.initLength
				}
			}
		case bytes.HasPrefix(lineBytes, []byte("#EXTINF:")):
			val := string(bytes.TrimPrefix(lineBytes, []byte("#EXTINF:")))
			if idx := strings.IndexByte(val, ','); idx != -1 {
				val = val[:idx]
			}
			dur, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err == nil && !math.IsNaN(dur) && !math.IsInf(dur, 0) && dur >= 0 && dur <= 86400 {
				currDuration = dur
			}
		case bytes.HasPrefix(lineBytes, []byte("#EXT-X-BYTERANGE:")):
			rangeSpec := string(bytes.TrimSpace(bytes.TrimPrefix(lineBytes, []byte("#EXT-X-BYTERANGE:"))))
			l, o, valid := parseByteRangeAttr(rangeSpec, true, nextByteRangeEnd)
			if !valid {
				return hlsPlaylist{}, fmt.Errorf("invalid #EXT-X-BYTERANGE %q", provider.Clean(rangeSpec))
			}
			nextByteRangeEnd = o + l
			for {
				nextLine, nextTail, ok := nextPlaylistLine(rest)
				if !ok {
					break
				}
				if bytes.HasPrefix(nextLine, []byte("#")) {
					rest = nextTail
					continue
				}
				if len(nextLine) > 0 {
					if len(parsed.segments) >= maxHLSSegments {
						return hlsPlaylist{}, fmt.Errorf("playlist exceeds max %d segments", maxHLSSegments)
					}
					rest = nextTail
					parsed.segments = append(parsed.segments, segmentRef{
						uri:      string(nextLine),
						offset:   o,
						length:   l,
						duration: currDuration,
					})
				}
				break
			}
		case lineBytes[0] != '#':
			if len(parsed.segments) >= maxHLSSegments {
				return hlsPlaylist{}, fmt.Errorf("playlist exceeds max %d segments", maxHLSSegments)
			}
			parsed.segments = append(parsed.segments, segmentRef{
				uri:      string(lineBytes),
				duration: currDuration,
			})
		}
	}
	return parsed, nil
}
