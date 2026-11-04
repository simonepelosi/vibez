//go:build linux || darwin || windows

package browserless

import (
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
	kid       []byte
	audioCfg  mp4.AudioConfig
	baseURI   string
	segments  []segmentRef
	totalDur  time.Duration
	createdAt time.Time

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

	mu          sync.RWMutex
	closed      bool
	closeCtx    context.Context
	closeCancel context.CancelFunc
	closeOnce   sync.Once
	bgWg        sync.WaitGroup
	streams     map[string]*trackStream
	serverCert  []byte

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

func (s *StreamServer) upstreamClient() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	if s.licClient != nil {
		return s.licClient.HTTPClient()
	}
	return &http.Client{Timeout: 15 * time.Second}
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
	lines := strings.Split(string(playlistBytes), "\n")
	var keyURI string
	var initURI string
	var initOffset, initLength int64
	var nextByteRangeEnd int64
	var segments []segmentRef
	var currDuration float64
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			if _, rest, ok := strings.Cut(line, "URI=\""); ok {
				if uri, _, ok := strings.Cut(rest, "\""); ok {
					keyURI = uri
				}
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			if _, rest, ok := strings.Cut(line, "URI=\""); ok {
				if uri, _, ok := strings.Cut(rest, "\""); ok {
					initURI = uri
				}
			}
			if _, rest, ok := strings.Cut(line, "BYTERANGE=\""); ok {
				if rangeStr, _, ok := strings.Cut(rest, "\""); ok {
					if lStr, oStr, hasAt := strings.Cut(rangeStr, "@"); hasAt {
						initLength, _ = strconv.ParseInt(strings.TrimSpace(lStr), 10, 64)
						initOffset, _ = strconv.ParseInt(strings.TrimSpace(oStr), 10, 64)
					} else {
						initLength, _ = strconv.ParseInt(strings.TrimSpace(rangeStr), 10, 64)
						initOffset = 0
					}
					nextByteRangeEnd = initOffset + initLength
				}
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			val := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.IndexByte(val, ','); idx != -1 {
				val = val[:idx]
			}
			dur, _ := strconv.ParseFloat(strings.TrimSpace(val), 64)
			currDuration = dur
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			rangeSpec := strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"))
			var l, o int64
			if lStr, oStr, hasAt := strings.Cut(rangeSpec, "@"); hasAt {
				l, _ = strconv.ParseInt(strings.TrimSpace(lStr), 10, 64)
				o, _ = strconv.ParseInt(strings.TrimSpace(oStr), 10, 64)
			} else {
				l, _ = strconv.ParseInt(rangeSpec, 10, 64)
				o = nextByteRangeEnd
			}
			nextByteRangeEnd = o + l
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "#") {
				i++
			}
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
				i++
				segURI := strings.TrimSpace(lines[i])
				segments = append(segments, segmentRef{
					uri:      segURI,
					offset:   o,
					length:   l,
					duration: currDuration,
				})
			}
		case !strings.HasPrefix(line, "#") && line != "":
			segments = append(segments, segmentRef{
				uri:      line,
				duration: currDuration,
			})
		}
	}

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
		kid:       kidBytes,
		audioCfg:  audioCfg,
		baseURI:   baseURI,
		segments:  segments,
		totalDur:  totalDur,
		createdAt: time.Now(),
	}

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

// register stores a prepared stream and returns its loopback URL. The sinks
// apply the AAC priming trim only to .aac streams.
func (s *StreamServer) register(ctx context.Context, streamID string, st *trackStream, ext string) string {
	s.mu.Lock()
	// Prune inactive/stale streams older than 2 hours
	now := time.Now()
	for id, stream := range s.streams {
		if now.Sub(stream.createdAt) > 2*time.Hour {
			delete(s.streams, id)
		}
	}
	s.streams[streamID] = st
	shouldWarm := !s.closed && len(st.segments) > 0 && st.segments[0].length <= maxMediaSegmentSize && (st.raw || s.cdmEngine != nil)
	baseCtx := ctx
	if s.closeCtx != nil {
		baseCtx = s.closeCtx
	}
	if shouldWarm {
		s.bgWg.Add(1)
	}
	s.mu.Unlock()

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

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	st, ok := s.streams[streamID]
	if !ok {
		s.mu.RUnlock()
		http.NotFound(w, r)
		return
	}
	closeCtx := s.closeCtx
	s.bgWg.Add(1)
	s.mu.RUnlock()
	defer s.bgWg.Done()

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

	for i := startSeg; i < len(st.segments); i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var aacData []byte
		if i == 0 {
			var stage string
			var err error
			aacData, stage, err = s.getOrFetchSegment0(ctx, client, st)
			if err != nil {
				fail(i, stage, err)
				return
			}
		} else {
			var stage string
			var err error
			aacData, stage, err = s.fetchAndDecryptSegment(ctx, client, st, st.segments[i])
			if err != nil {
				fail(i, stage, err)
				return
			}
		}

		// Write to HTTP client (GStreamer). A failed write means the client
		// has gone, so there is nobody left to abort for; log it only.
		if _, err := w.Write(aacData); err != nil {
			if ctx.Err() == nil && s.onLog != nil {
				s.onLog(fmt.Sprintf("browserless stream: %s: segment %d/%d: write: %v", st.trackID, i+1, len(st.segments), err))
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
	aacData, err := mp4.DecryptSegment(segData, st.kid, st.audioCfg, func(kid, iv, input []byte, subs []mp4.Subsample) ([]byte, error) {
		cdmSubs := make([]cdm.Subsample, len(subs))
		for si, sb := range subs {
			cdmSubs[si] = cdm.Subsample{
				ClearBytes:  sb.ClearBytes,
				CipherBytes: sb.CipherBytes,
			}
		}
		return s.cdmEngine.DecryptSubsamples(kid, iv, input, cdmSubs)
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

		segData, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
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
		s.mu.Unlock()
		if s.server != nil {
			err = s.server.Close()
		}
		s.bgWg.Wait()
	})
	return err
}
