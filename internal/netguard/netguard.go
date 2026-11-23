// Package netguard provides shared SSRF, special-use IP, and safe URL/dialer
// validation helpers used across network clients and desktop integrations.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	nat64WellKnownPrefix = netip.MustParsePrefix("64:ff9b::/96")

	// literalOnlyUnsafePrefixes blocks address ranges that never appear as
	// literal hosts in public URLs, while remaining permitted after DNS
	// resolution for clash/sing-box TUN fake-IP (198.18.0.0/15) and NAT64
	// DNS64-synthesized IPv6 (64:ff9b::/96, whose embedded IPv4 is still checked
	// by IsUnsafeAddr).
	literalOnlyUnsafePrefixes = mustParsePrefixes([]string{
		"198.18.0.0/15", // Benchmarking / TUN fake-IP
		"64:ff9b::/96",  // NAT64 well-known prefix (RFC 6052)
	})

	unsafePrefixes = mustParsePrefixes([]string{
		// IPv4 special-use & non-routable ranges
		"0.0.0.0/8",      // "This" network (routed to loopback on Linux/BSD)
		"10.0.0.0/8",     // RFC 1918 private
		"100.64.0.0/10",  // RFC 6598 Shared Address Space / CGNAT / overlay VPNs
		"127.0.0.0/8",    // Loopback
		"169.254.0.0/16", // Link-local / cloud metadata
		"172.16.0.0/12",  // RFC 1918 private
		"192.0.0.0/24",   // IETF protocol assignments
		"192.0.2.0/24",   // TEST-NET-1
		"192.88.99.0/24", // Deprecated 6to4 relay
		"192.168.0.0/16", // RFC 1918 private
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4", // Multicast
		"240.0.0.0/4", // Reserved & 255.255.255.255 limited broadcast
		"::/128",      // Unspecified
		"::1/128",     // Loopback
		"::/96",       // Deprecated IPv4-compatible IPv6
		"64:ff9b:1::/48",
		"100::/64",    // Discard-only
		"2001::/32",   // Teredo
		"2001:2::/48", // Benchmarking
		"2001:db8::/32",
		"2002::/16", // 6to4
		"fc00::/7",  // Unique local addresses (ULA)
		"fe80::/10", // Link-local unicast
		"fec0::/10", // Deprecated site-local
		"ff00::/8",  // Multicast
	})
)

func mustParsePrefixes(raw []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// IsUnsafeAddr reports whether addr is invalid, has an IPv6 zone identifier, or
// belongs to a non-public, loopback, private, CGNAT, transition, link-local,
// multicast, or reserved address range at dial time. TUN fake-IP (198.18.0.0/15)
// and NAT64 well-known prefix (64:ff9b::/96 when wrapping a public IPv4) are
// allowed at dial time so TUN proxies and DNS64 networks keep working.
func IsUnsafeAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return true
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() ||
		addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() {
		return true
	}
	if nat64WellKnownPrefix.Contains(addr) {
		raw := addr.As16()
		embedded := netip.AddrFrom4([4]byte(raw[12:16]))
		return IsUnsafeAddr(embedded)
	}
	for _, pfx := range unsafePrefixes {
		if pfx.Contains(addr) {
			return true
		}
	}
	return false
}

// IsUnsafeLiteralAddr reports whether addr is unsafe either at dial time or as
// an explicit URL literal (additionally rejecting TUN fake-IP 198.18.0.0/15
// and NAT64 64:ff9b::/96 literals that should never appear in public URLs).
func IsUnsafeLiteralAddr(addr netip.Addr) bool {
	if IsUnsafeAddr(addr) {
		return true
	}
	if addr.IsValid() {
		addr = addr.Unmap()
	}
	for _, pfx := range literalOnlyUnsafePrefixes {
		if pfx.Contains(addr) {
			return true
		}
	}
	return false
}

// IsUnsafeIP reports whether ip is nil or unsafe as an explicit URL address.
func IsUnsafeIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return IsUnsafeLiteralAddr(addr)
}

func isNumericIPv4Label(label string) bool {
	if label == "" {
		return false
	}
	if strings.HasPrefix(label, "0x") || strings.HasPrefix(label, "0X") {
		if len(label) <= 2 {
			return false
		}
		for i := 2; i < len(label); i++ {
			c := label[i]
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
		_, err := strconv.ParseUint(label, 0, 64)
		return err == nil
	}
	for i := 0; i < len(label); i++ {
		if label[i] < '0' || label[i] > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(label, 0, 64)
	return err == nil
}

// IsNonCanonicalIPv4Host detects non-canonical numeric IPv4 representations
// such as integer (2130706433), hex (0x7f000001), octal (0177.0.0.1), or
// shorthand dotted forms (127.1).
func IsNonCanonicalIPv4Host(host string) bool {
	host = strings.TrimSuffix(host, ".")
	parts := strings.Split(host, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if !isNumericIPv4Label(p) {
			return false
		}
	}
	return true
}

// IsLoopbackHost reports whether host denotes localhost or a loopback literal
// without an IPv6 zone identifier.
func IsLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" {
		return true
	}
	if strings.ContainsRune(h, '%') {
		return false
	}
	addr, err := netip.ParseAddr(h)
	return err == nil && addr.Zone() == "" && addr.Unmap().IsLoopback()
}

// IsBlockedHostname reports whether a parsed URL hostname targets a local,
// reserved, zone-scoped, non-canonical IPv4, or non-public address.
func IsBlockedHostname(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "" || strings.ContainsRune(h, '%') {
		return true
	}
	if h == "localhost" ||
		h == "localhost.localdomain" ||
		strings.HasSuffix(h, ".localhost") ||
		strings.HasSuffix(h, ".local") ||
		strings.HasSuffix(h, ".internal") {
		return true
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		return IsUnsafeLiteralAddr(addr)
	}
	return IsNonCanonicalIPv4Host(h)
}

// HasUnsafeControlRunes reports whether s is invalid UTF-8 or contains C0/C1/DEL
// control characters or invisible Unicode format/BiDi characters (`Cf`).
func HasUnsafeControlRunes(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// ParseSafeURL validates rawURL against length/control limits, userinfo rules,
// scheme rules (HTTPS, plus loopback HTTP only when allowLoopbackHTTP is true),
// and hostname/IP SSRF filters.
func ParseSafeURL(rawURL string, maxLen int, allowLoopbackHTTP bool) (*url.URL, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("empty URL")
	}
	if maxLen > 0 && len(rawURL) > maxLen {
		return nil, fmt.Errorf("URL exceeds maximum length %d", maxLen)
	}
	if HasUnsafeControlRunes(rawURL) {
		return nil, fmt.Errorf("URL contains unsafe or invalid characters")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Host == "" || u.User != nil {
		return nil, fmt.Errorf("URL must include host without userinfo")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL missing hostname")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if allowLoopbackHTTP && IsLoopbackHost(host) {
			return u, nil
		}
		if IsBlockedHostname(host) {
			return nil, fmt.Errorf("refusing non-public host %q", host)
		}
		return u, nil
	case "http":
		if allowLoopbackHTTP && IsLoopbackHost(host) {
			return u, nil
		}
		return nil, fmt.Errorf("disallowed insecure scheme %q", u.Scheme)
	default:
		return nil, fmt.Errorf("disallowed URL scheme %q", u.Scheme)
	}
}

// IsSafePublicHTTPSURL reports whether rawURL is a clean, bounded public HTTPS
// URL (or loopback HTTP/HTTPS when allowLoopbackHTTP is true).
func IsSafePublicHTTPSURL(rawURL string, maxLen int, allowLoopbackHTTP bool) bool {
	_, err := ParseSafeURL(rawURL, maxLen, allowLoopbackHTTP)
	return err == nil
}

// SafeDialControl returns a net.Dialer Control hook rejecting resolved IP
// addresses that are non-public (or allowing loopback only when allowLoopback
// is true).
func SafeDialControl(allowLoopback bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("refusing unparseable resolved IP %q", host)
		}
		if addr.Zone() != "" {
			return fmt.Errorf("refusing zone-scoped IP address")
		}
		if allowLoopback && addr.Unmap().IsLoopback() {
			return nil
		}
		if IsUnsafeAddr(addr) {
			return fmt.Errorf("refusing connection to non-public address")
		}
		return nil
	}
}

// NewSafeDialContext returns a DialContext function bound to SafeDialControl.
func NewSafeDialContext(timeout time.Duration, allowLoopback bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: timeout,
		Control: SafeDialControl(allowLoopback),
	}
	return dialer.DialContext
}

type allowedProxyContextKey struct{}

type safeRoundTripper struct {
	base          *http.Transport
	allowLoopback bool
}

// NewSafeRoundTripper returns an http.RoundTripper cloned from base that
// validates each request URL via ParseSafeURL and enforces SafeDialControl on
// non-proxied TCP dials while exempting only the explicitly configured proxy
// target returned by base.Proxy (for example HTTP_PROXY/HTTPS_PROXY on
// loopback, LAN RFC 1918, or CGNAT Tailscale hosts).
func NewSafeRoundTripper(base *http.Transport, dialTimeout time.Duration, allowLoopback bool) http.RoundTripper {
	var tr *http.Transport
	if base != nil {
		tr = base.Clone()
		if base.TLSNextProto == nil {
			// base.Clone() triggers onceSetNextProtoDefaults on transports with
			// DialContext == nil, populating ALPN "h2" in TLSClientConfig. Keep
			// HTTP/2 enabled after installing our custom DialContext so Go does
			// not negotiate "h2" and then parse binary HTTP/2 frames as HTTP/1.x.
			tr.ForceAttemptHTTP2 = true
		}
	} else {
		tr = &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			ForceAttemptHTTP2: true,
		}
	}
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	directDialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	safeDialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
		Control:   SafeDialControl(allowLoopback),
	}
	tr.DialTLSContext = nil
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowedProxy, ok := ctx.Value(allowedProxyContextKey{}).(string); ok && allowedProxy != "" && sameDialTarget(addr, allowedProxy) {
			return directDialer.DialContext(ctx, network, addr)
		}
		return safeDialer.DialContext(ctx, network, addr)
	}
	return &safeRoundTripper{
		base:          tr,
		allowLoopback: allowLoopback,
	}
}

func (s *safeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, fmt.Errorf("netguard: nil request or URL")
	}
	if _, err := ParseSafeURL(req.URL.String(), 0, s.allowLoopback); err != nil {
		return nil, err
	}
	if s.base.Proxy != nil {
		proxyURL, err := s.base.Proxy(req)
		if err != nil {
			return nil, err
		}
		if proxyAddr := canonicalProxyAddr(proxyURL); proxyAddr != "" {
			req = req.Clone(context.WithValue(req.Context(), allowedProxyContextKey{}, proxyAddr))
		}
	}
	return s.base.RoundTrip(req)
}

func (s *safeRoundTripper) CloseIdleConnections() {
	s.base.CloseIdleConnections()
}

func canonicalProxyAddr(proxyURL *url.URL) string {
	if proxyURL == nil {
		return ""
	}
	host := proxyURL.Hostname()
	if host == "" {
		return ""
	}
	port := proxyURL.Port()
	if port == "" {
		switch strings.ToLower(proxyURL.Scheme) {
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}

func sameDialTarget(a, b string) bool {
	ha, pa, errA := net.SplitHostPort(a)
	hb, pb, errB := net.SplitHostPort(b)
	if errA != nil || errB != nil || pa != pb {
		return false
	}
	if addrA, err := netip.ParseAddr(ha); err == nil {
		if addrB, err := netip.ParseAddr(hb); err == nil {
			return addrA.Unmap() == addrB.Unmap()
		}
	}
	return strings.EqualFold(ha, hb)
}
