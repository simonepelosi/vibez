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
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/player/browserless/cdm"
	"github.com/simone-vibes/vibez/internal/player/browserless/license"
	"github.com/simone-vibes/vibez/internal/player/browserless/mp4"
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

	seg0Mu  sync.Mutex
	seg0AAC []byte
}

const (
	maxHLSPlaylistBytes = 4 << 20  // 4 MiB cap on HLS media playlist
	maxInitSegmentBytes = 4 << 20  // 4 MiB cap on MP4 init segment
	maxMediaSegmentSize = 64 << 20 // 64 MiB cap on a single HLS media segment
	maxRawUploadSize    = 2 << 30  // 2 GiB ceiling on a non-seekable whole-file upload
	maxRetainedStreams  = 3
	maxStreamAge        = 30 * time.Minute
)

// StreamServer manages local HTTP streaming for decrypted Apple Music tracks.
type StreamServer struct {
	cdmEngine  *cdm.CDM
	licClient  *license.Client
	httpClient *http.Client
	listener   net.Listener
	server     *http.Server
	port       int
	authToken  string

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

var defaultUpstreamClient = &http.Client{Timeout: 15 * time.Second}

func (s *StreamServer) upstreamClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	if s.licClient != nil {
		return s.licClient.HTTPClient()
	}
	return defaultUpstreamClient
}

// PrepareTrack resolves Apple Music playback metadata, acquires the Widevine license,
// parses the HLS manifest, and returns a local HTTP streaming URL and total duration.
func (s *StreamServer) PrepareTrack(ctx context.Context, trackID string, prefer256k bool) (string, time.Duration, error) {
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
	parsed := parseHLSPlaylist(playlistBytes)
	keyURI := parsed.keyURI
	initURI := parsed.initURI
	initOffset, initLength := parsed.initOffset, parsed.initLength
	segments := parsed.segments

	if keyURI == "" {
		return "", 0, fmt.Errorf("no key URI found in playlist")
	}
	b64KID := strings.TrimPrefix(keyURI, "data:;base64,")
	kidBytes, err := base64.StdEncoding.DecodeString(b64KID)
	if err != nil || len(kidBytes) != 16 {
		return "", 0, fmt.Errorf("invalid KID (%s): %w", keyURI, err)
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
	if lastSlash := strings.LastIndex(baseURI, "/"); lastSlash != -1 {
		baseURI = baseURI[:lastSlash+1]
	}

	var initBytes []byte
	if initURI != "" {
		fullInitURL := baseURI + initURI
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

	head, size, err := fetchRawHeadWithClient(ctx, s.upstreamClient(), assetURL)
	if err != nil {
		return "", 0, fmt.Errorf("fetch upload: %w", err)
	}
	format, ok := sniffRawFormat(head, size)
	if !ok {
		return "", 0, fmt.Errorf("%w: unsupported upload format (library item %s)", license.ErrTrackUnavailable, libraryID)
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
	return fetchRawHeadWithClient(ctx, &http.Client{Timeout: 15 * time.Second}, assetURL)
}

func fetchRawHeadWithClient(ctx context.Context, client *http.Client, assetURL string) ([]byte, int64, error) {
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
			size, _ = strconv.ParseInt(total, 10, 64)
		}
	case http.StatusOK:
		size = resp.ContentLength
	default:
		return nil, 0, fmt.Errorf("status %s", resp.Status)
	}
	if size <= 0 {
		return nil, 0, fmt.Errorf("unknown size")
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
			ev.stream.seg0AAC = nil
			ev.stream.seg0Mu.Unlock()
			ev.stream.segments = nil
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
	shouldWarm := !s.closed && len(st.segments) > 0 && st.segments[0].length <= maxMediaSegmentSize && (st.raw || s.cdmEngine != nil)
	baseCtx := ctx
	if s.closeCtx != nil {
		baseCtx = s.closeCtx
	}
	if shouldWarm {
		s.bgWg.Add(1)
	}
	s.mu.Unlock()
	s.releaseStreams(evicted)

	// Warm segment 0 asynchronously so both initial playback and
	// seek-to-start (0:00) can write the first ~10s immediately
	// without waiting for an upstream CDN round-trip.
	if shouldWarm {
		go func() {
			defer s.bgWg.Done()
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
	st, ok := s.streams[streamID]
	s.mu.RUnlock()
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
		if sec, err := strconv.ParseFloat(qTime, 64); err == nil && sec > 0 {
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

	type segResult struct {
		aacData []byte
		stage   string
		err     error
	}

	lookCtx, cancelLook := context.WithCancel(ctx)
	var lookWg sync.WaitGroup
	defer func() {
		cancelLook()
		lookWg.Wait()
	}()

	fetchSeg := func(fetchCtx context.Context, idx int) segResult {
		if idx == 0 {
			data, stage, err := s.getOrFetchSegment0(fetchCtx, client, st)
			return segResult{aacData: data, stage: stage, err: err}
		}
		data, stage, err := s.fetchAndDecryptSegment(fetchCtx, client, st, st.segments[idx])
		return segResult{aacData: data, stage: stage, err: err}
	}

	var nextCh <-chan segResult
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
		if _, err := w.Write(res.aacData); err != nil {
			if ctx.Err() == nil && s.onLog != nil {
				s.onLog(fmt.Sprintf("browserless stream: %s: segment %d/%d: write: %v", st.trackID, i+1, len(st.segments), err))
			}
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		res.aacData = nil
	}
}

func (s *StreamServer) getOrFetchSegment0(ctx context.Context, client *http.Client, st *trackStream) ([]byte, string, error) {
	st.seg0Mu.Lock()
	defer st.seg0Mu.Unlock()
	if len(st.seg0AAC) > 0 {
		return st.seg0AAC, "", nil
	}
	aacData, stage, err := s.fetchAndDecryptSegment(ctx, client, st, st.segments[0])
	if err != nil {
		return nil, stage, err
	}
	st.seg0AAC = aacData
	return aacData, "", nil
}

func (s *StreamServer) fetchAndDecryptSegment(ctx context.Context, client *http.Client, st *trackStream, seg segmentRef) ([]byte, string, error) {
	segURL := seg.uri
	if !strings.HasPrefix(segURL, "http://") && !strings.HasPrefix(segURL, "https://") {
		segURL = st.baseURI + seg.uri
	}

	maxBytes := int64(maxMediaSegmentSize)
	reqClient := client
	if st.raw && seg.length > 0 {
		// Non-seekable uploads (e.g. M4A mixes/live sets) go out as a single
		// segment the size of the file; cap at the HEAD-reported length (up to
		// maxRawUploadSize) rather than the 64 MiB per-HLS-segment bound.
		maxBytes = min(seg.length, int64(maxRawUploadSize))
		if seg.length > maxMediaSegmentSize && client != nil && client.Timeout > 0 {
			cCopy := *client
			cCopy.Timeout = 0
			reqClient = &cCopy
		}
	}

	segData, stage, err := fetchSegmentWithRetryMax(ctx, reqClient, segURL, seg.offset, seg.length, maxBytes)
	if err != nil {
		return nil, stage, err
	}
	if st.raw {
		return segData, "", nil
	}
	var cdmScratch cdm.DecryptScratch
	var cdmSubs []cdm.Subsample
	aacData, err := mp4.DecryptSegment(segData, st.kid, st.audioCfg, func(kid, iv, input []byte, subs []mp4.Subsample) ([]byte, error) {
		if cap(cdmSubs) < len(subs) {
			cdmSubs = make([]cdm.Subsample, len(subs))
		}
		cdmSubs = cdmSubs[:len(subs)]
		for si, sb := range subs {
			cdmSubs[si] = cdm.Subsample{
				ClearBytes:  sb.ClearBytes,
				CipherBytes: sb.CipherBytes,
			}
		}
		return s.cdmEngine.DecryptSubsamplesReuse(&cdmScratch, kid, iv, input, cdmSubs)
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

		segData, err := readBoundedBody(resp.Body, max(resp.ContentLength, length), maxBytes)
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

func readBoundedBody(r io.Reader, sizeHint, maxBytes int64) ([]byte, error) {
	hint := min(max(sizeHint, 0), min(maxBytes, maxMediaSegmentSize))
	var buf bytes.Buffer
	if hint > 0 {
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

func parseHLSPlaylist(playlistBytes []byte) hlsPlaylist {
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
					if lStr, oStr, hasAt := strings.Cut(rangeStr, "@"); hasAt {
						parsed.initLength, _ = strconv.ParseInt(strings.TrimSpace(lStr), 10, 64)
						parsed.initOffset, _ = strconv.ParseInt(strings.TrimSpace(oStr), 10, 64)
					} else {
						parsed.initLength, _ = strconv.ParseInt(strings.TrimSpace(rangeStr), 10, 64)
						parsed.initOffset = 0
					}
					nextByteRangeEnd = parsed.initOffset + parsed.initLength
				}
			}
		case bytes.HasPrefix(lineBytes, []byte("#EXTINF:")):
			val := string(bytes.TrimPrefix(lineBytes, []byte("#EXTINF:")))
			if idx := strings.IndexByte(val, ','); idx != -1 {
				val = val[:idx]
			}
			currDuration, _ = strconv.ParseFloat(strings.TrimSpace(val), 64)
		case bytes.HasPrefix(lineBytes, []byte("#EXT-X-BYTERANGE:")):
			rangeSpec := string(bytes.TrimSpace(bytes.TrimPrefix(lineBytes, []byte("#EXT-X-BYTERANGE:"))))
			var l, o int64
			if lStr, oStr, hasAt := strings.Cut(rangeSpec, "@"); hasAt {
				l, _ = strconv.ParseInt(strings.TrimSpace(lStr), 10, 64)
				o, _ = strconv.ParseInt(strings.TrimSpace(oStr), 10, 64)
			} else {
				l, _ = strconv.ParseInt(rangeSpec, 10, 64)
				o = nextByteRangeEnd
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
			parsed.segments = append(parsed.segments, segmentRef{
				uri:      string(lineBytes),
				duration: currDuration,
			})
		}
	}
	return parsed
}
