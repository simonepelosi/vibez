//go:build windows

package local

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

// mediaTypes are the Content-Types served for the formats the provider
// indexes, plus WAV. An unknown extension goes out as octet-stream and is
// left to Chrome's own sniffing.
var mediaTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".flac": "audio/flac",
	".m4a":  "audio/mp4",
	".ogg":  "audio/ogg",
	".wav":  "audio/wav",
}

// mediaServer is the HTTP server Chrome loads the player page and library
// files from. It listens on 127.0.0.1 only, on a port the OS picks, and
// answers only under a random per-session path prefix and to its own Host.
// A file is reachable only through the opaque route ID minted when its track
// was registered, so no part of a request path is ever mapped onto the
// filesystem, and a path's spaces, '#' or non-ASCII never appear in a URL.
type mediaServer struct {
	srv    *http.Server
	host   string // 127.0.0.1:port, the only Host header answered
	prefix string // "/<session token>/"

	mu     sync.RWMutex
	routes map[string]string // route ID → file path
	ids    map[string]string // track ID → route ID
}

func newMediaServer() (*mediaServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("media server: %w", err)
	}
	m := &mediaServer{
		host:   ln.Addr().String(),
		prefix: "/" + rand.Text() + "/",
		routes: map[string]string{},
		ids:    map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+m.prefix+"{$}", serveAsset("text/html; charset=utf-8", pageHTML))
	mux.HandleFunc("GET "+m.prefix+"engine.js", serveAsset("text/javascript; charset=utf-8", engineJS))
	mux.HandleFunc("GET "+m.prefix+"media/{id}", m.serveMedia)
	m.srv = &http.Server{
		Handler:           m.checkHost(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       time.Minute,
	}
	go func() { _ = m.srv.Serve(ln) }()
	return m, nil
}

func (m *mediaServer) pageURL() string { return "http://" + m.host + m.prefix }

// url returns the address Chrome fetches the track with ID id from, and false
// when id was not registered.
func (m *mediaServer) url(id string) (string, bool) {
	m.mu.RLock()
	route, ok := m.ids[id]
	m.mu.RUnlock()
	if !ok {
		return "", false
	}
	return m.pageURL() + "media/" + route, true
}

// setTracks makes the files behind tracks, and only those, reachable. A
// track registered before keeps its route, so a file Chrome is streaming
// stays reachable across a rescan that still lists it.
func (m *mediaServer) setTracks(tracks []provider.Track) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make(map[string]string, len(tracks))
	routes := make(map[string]string, len(tracks))
	for _, t := range tracks {
		path, ok := strings.CutPrefix(t.ID, "local:")
		if !ok || path == "" {
			continue
		}
		if _, dup := ids[t.ID]; dup {
			continue
		}
		route, ok := m.ids[t.ID]
		if !ok {
			route = rand.Text()
		}
		ids[t.ID] = route
		routes[route] = path
	}
	m.ids = ids
	m.routes = routes
}

func (m *mediaServer) close() { _ = m.srv.Close() }

// checkHost refuses requests addressed to any other host, so a page elsewhere
// cannot reach the server through a DNS name rebound to 127.0.0.1.
func (m *mediaServer) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Host), []byte(m.host)) != 1 {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveAsset(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", contentType)
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	}
}

// serveMedia streams a registered file. http.ServeContent answers the Range
// requests Chrome makes to seek and to read container indexes at the end of
// a file. Failures say nothing about the path.
func (m *mediaServer) serveMedia(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	path, ok := m.routes[r.PathValue("id")]
	m.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path) //nolint:gosec // path is a registered library file, never request input
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	contentType, ok := mediaTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		contentType = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
