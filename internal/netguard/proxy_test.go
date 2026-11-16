package netguard

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsUnsafeAddr_AllowsTUNFakeIPAndPublicNAT64WhileBlockingLiterals(t *testing.T) {
	tunIP := netip.MustParseAddr("198.18.0.1")
	if IsUnsafeAddr(tunIP) {
		t.Errorf("IsUnsafeAddr(%s) = true, want false for TUN fake-IP", tunIP)
	}
	if !IsUnsafeLiteralAddr(tunIP) {
		t.Errorf("IsUnsafeLiteralAddr(%s) = false, want true", tunIP)
	}
	if !IsUnsafeIP(net.ParseIP("198.18.0.1")) {
		t.Error("IsUnsafeIP(198.18.0.1) = false, want true for literal check")
	}

	nat64Public := netip.MustParseAddr("64:ff9b::93.184.216.34")
	if IsUnsafeAddr(nat64Public) {
		t.Errorf("IsUnsafeAddr(%s) = true, want false for DNS64 public IPv4", nat64Public)
	}
	if !IsUnsafeLiteralAddr(nat64Public) {
		t.Errorf("IsUnsafeLiteralAddr(%s) = false, want true", nat64Public)
	}

	nat64Private := netip.MustParseAddr("64:ff9b::169.254.169.254")
	if !IsUnsafeAddr(nat64Private) {
		t.Errorf("IsUnsafeAddr(%s) = false, want true for DNS64 metadata IPv4", nat64Private)
	}

	for _, rawURL := range []string{
		"https://198.18.0.1/art.jpg",
		"https://[64:ff9b::93.184.216.34]/art.jpg",
	} {
		if _, err := ParseSafeURL(rawURL, 2048, false); err == nil {
			t.Errorf("ParseSafeURL(%q, allowLoopback=false) = nil, want error", rawURL)
		}
	}

	strict := SafeDialControl(false)
	for _, target := range []string{"198.18.0.1:443", "[64:ff9b::93.184.216.34]:443"} {
		if err := strict("tcp", target, nil); err != nil {
			t.Errorf("SafeDialControl(false)(%q) = %v, want nil", target, err)
		}
	}
}

func TestNewSafeRoundTripper_AllowsConfiguredLoopbackProxyAndBlocksSSRF(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "proxied-ok")
	}))

	var proxyCalls atomic.Int32
	var wg sync.WaitGroup

	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.Method != http.MethodConnect {
			http.Error(w, "expected CONNECT", http.StatusMethodNotAllowed)
			return
		}
		backendConn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(r.Context(), "tcp", upstream.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = backendConn.Close()
			http.Error(w, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		clientConn, bufrw, err := hijacker.Hijack()
		if err != nil {
			_ = backendConn.Close()
			return
		}
		if _, err := bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			_ = clientConn.Close()
			_ = backendConn.Close()
			return
		}
		if err := bufrw.Flush(); err != nil {
			_ = clientConn.Close()
			_ = backendConn.Close()
			return
		}

		wg.Go(func() {
			defer func() { _ = clientConn.Close() }()
			defer func() { _ = backendConn.Close() }()

			done := make(chan struct{})
			go func() {
				defer close(done)
				if bufrw.Reader.Buffered() > 0 {
					_, _ = io.CopyN(backendConn, bufrw, int64(bufrw.Reader.Buffered()))
				}
				_, _ = io.Copy(backendConn, clientConn)
				_ = backendConn.Close()
			}()
			_, _ = io.Copy(clientConn, backendConn)
			_ = clientConn.Close()
			<-done
		})
	}))

	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}

	upstreamTransport, ok := upstream.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected upstream transport type %T", upstream.Client().Transport)
	}

	var bypassProxy atomic.Bool
	baseTransport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			if bypassProxy.Load() {
				return nil, nil
			}
			return http.ProxyURL(proxyURL)(req)
		},
		TLSClientConfig: &tls.Config{
			RootCAs:    upstreamTransport.TLSClientConfig.RootCAs,
			ServerName: "127.0.0.1",
			MinVersion: tls.VersionTLS12,
		},
	}

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: NewSafeRoundTripper(baseTransport, 2*time.Second, false),
	}
	defer func() {
		client.CloseIdleConnections()
		proxySrv.CloseClientConnections()
		proxySrv.Close()
		upstream.Close()
		wg.Wait()
	}()

	// Configured loopback proxy must work even with allowLoopback=false when destination URL is public HTTPS.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://is1-ssl.mzstatic.com/cover.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxied public HTTPS request failed: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "proxied-ok" {
		t.Fatalf("response body = %q, want %q", body, "proxied-ok")
	}
	if proxyCalls.Load() != 1 {
		t.Fatalf("proxyCalls = %d, want 1", proxyCalls.Load())
	}

	// Destination URL validation must still block SSRF targets before touching the proxy.
	for _, unsafeURL := range []string{
		"https://localhost/admin",
		"https://127.0.0.1/admin",
		"https://198.18.0.1/admin",
		"https://[64:ff9b::127.0.0.1]/admin",
		"http://is1-ssl.mzstatic.com/cover.jpg",
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, unsafeURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if r, err := client.Do(req); err == nil {
			_ = r.Body.Close()
			t.Errorf("proxied request to %q unexpectedly succeeded", unsafeURL)
		}
	}
	if proxyCalls.Load() != 1 {
		t.Fatalf("proxyCalls after blocked SSRF URLs = %d, want 1", proxyCalls.Load())
	}

	// Non-proxied dial (e.g. NO_PROXY match) or mismatched port must still reject unsafe IPs.
	bypassProxy.Store(true)
	rt, ok := client.Transport.(*safeRoundTripper)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if conn, err := rt.base.DialContext(t.Context(), "tcp", upstream.Listener.Addr().String()); err == nil {
		_ = conn.Close()
		t.Fatal("DialContext without configured proxy allowed loopback dial")
	}

	mismatchedCtx := t.Context()
	// Verify dialing a different loopback host:port than the configured proxy is still blocked.
	reqDirect := httptest.NewRequest(http.MethodGet, "https://is1-ssl.mzstatic.com/cover.jpg", nil).WithContext(mismatchedCtx)
	if _, err := NewSafeRoundTripper(nil, time.Second, false).(*safeRoundTripper).base.DialContext(reqDirect.Context(), "tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("nil-base NewSafeRoundTripper allowed unproxied loopback dial")
	}
}
