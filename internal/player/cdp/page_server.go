package cdp

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"time"
)

// pageServer serves the rendered MusicKit page to the Chrome instance vibez
// launches, and to nobody else. It listens on 127.0.0.1 on a port the OS
// picks, answers only GET <prefix> where prefix is a per-session random path,
// and answers only to its own Host header, so neither another program on the
// machine that merely finds the port nor a web page reaching it through a DNS
// name rebound to 127.0.0.1 can fetch the page.
//
// The page itself holds the developer token only; the user's Music-User-Token
// is handed to it over the goGetUserToken binding and is never served here.
type pageServer struct {
	srv    *http.Server
	host   string // 127.0.0.1:port, the only Host header answered
	prefix string // "/<session token>/"
}

func newPageServer(html string) (*pageServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("cdp: listen: %w", err)
	}
	s := &pageServer{
		host:   ln.Addr().String(),
		prefix: "/" + rand.Text() + "/",
	}
	s.srv = &http.Server{
		Handler:           s.handler(html),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

func (s *pageServer) handler(html string) http.Handler {
	body := []byte(html)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Host), []byte(s.host)) != 1 ||
			r.Method != http.MethodGet ||
			r.URL.Path != s.prefix {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body)
	})
}

// pageURL is the address Chrome loads the player page from.
func (s *pageServer) pageURL() string { return "http://" + s.host + s.prefix }

func (s *pageServer) close() { _ = s.srv.Close() }
