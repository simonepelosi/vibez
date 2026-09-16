//go:build linux

package browserless

import (
	"context"
	"crypto/rand"
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
}

// StreamServer manages local HTTP streaming for decrypted Apple Music tracks.
type StreamServer struct {
	cdmEngine *cdm.CDM
	licClient *license.Client
	listener  net.Listener
	server    *http.Server
	port      int
	authToken string

	mu         sync.RWMutex
	streams    map[string]*trackStream
	serverCert []byte
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

	port := ln.Addr().(*net.TCPAddr).Port
	s := &StreamServer{
		cdmEngine: cdmEngine,
		licClient: licClient,
		listener:  ln,
		port:      port,
		authToken: authToken,
		streams:   make(map[string]*trackStream),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", s.handleStream)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Silence broken-pipe and connection-reset logs on skip so they don't corrupt the TUI.
		ErrorLog: log.New(io.Discard, "", 0),
	}

	go func() {
		_ = s.server.Serve(ln)
	}()

	return s, nil
}

// PrepareTrack resolves Apple Music playback metadata, acquires the Widevine license,
// parses the HLS manifest, and returns a local HTTP streaming URL and total duration.
func (s *StreamServer) PrepareTrack(ctx context.Context, trackID string, prefer256k bool) (string, time.Duration, error) {
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
	hlsResp, err := http.DefaultClient.Do(hlsReq)
	if err != nil {
		return "", 0, fmt.Errorf("fetch playlist: %w", err)
	}
	defer func() { _ = hlsResp.Body.Close() }()

	if hlsResp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("fetch playlist status %d: %s", hlsResp.StatusCode, hlsResp.Status)
	}

	playlistBytes, err := io.ReadAll(hlsResp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read playlist: %w", err)
	}

	// 4. Parse HLS playlist
	lines := strings.Split(string(playlistBytes), "\n")
	var keyURI string
	var initURI string
	var initOffset, initLength int64
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
					_, _ = fmt.Sscanf(rangeStr, "%d@%d", &initLength, &initOffset)
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
			var l, o int64
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"), "%d@%d", &l, &o)
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
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fullInitURL, nil)
		if initLength > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", initOffset, initOffset+initLength-1))
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
				initBytes, _ = io.ReadAll(resp.Body)
			}
			_ = resp.Body.Close()
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

	s.mu.Lock()
	// Prune inactive/stale streams older than 2 hours
	now := time.Now()
	for id, stream := range s.streams {
		if now.Sub(stream.createdAt) > 2*time.Hour {
			delete(s.streams, id)
		}
	}
	s.streams[streamID] = st
	s.mu.Unlock()

	streamURL := fmt.Sprintf("http://127.0.0.1:%d/stream/%s/%s.aac", s.port, streamID, s.authToken)
	return streamURL, totalDur, nil
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

	// Path format: /stream/{streamID}/{token}.aac
	path := strings.TrimPrefix(r.URL.Path, "/stream/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	streamID := parts[0]
	filePart := parts[1]
	token := strings.TrimSuffix(filePart, ".aac")
	if token != s.authToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	st, ok := s.streams[streamID]
	s.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

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

	w.Header().Set("Content-Type", "audio/aac")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	ctx := r.Context()
	client := &http.Client{Timeout: 15 * time.Second}

	for i := startSeg; i < len(st.segments); i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		seg := st.segments[i]
		segURL := seg.uri
		if !strings.HasPrefix(segURL, "http://") && !strings.HasPrefix(segURL, "https://") {
			segURL = st.baseURI + seg.uri
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
		if err != nil {
			return
		}
		if seg.length > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", seg.offset, seg.offset+seg.length-1))
		}

		resp, err := client.Do(req)
		if err != nil {
			return
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			_ = resp.Body.Close()
			return
		}

		segData, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return
		}

		// Decrypt segment into ADTS AAC frames passing subsamples
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
			return
		}

		// Write to HTTP client (GStreamer)
		if _, err := w.Write(aacData); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// Close terminates the local HTTP stream server.
func (s *StreamServer) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}
