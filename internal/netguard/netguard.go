// Package netguard provides shared SSRF, special-use IP, and safe URL/dialer
// validation helpers used across network clients and desktop integrations.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

var unsafePrefixes = mustParsePrefixes([]string{
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
	"198.18.0.0/15",  // Benchmarking
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4", // Multicast
	"240.0.0.0/4", // Reserved & 255.255.255.255 limited broadcast
	"::/128",      // Unspecified
	"::1/128",     // Loopback
	"::/96",       // Deprecated IPv4-compatible IPv6
	"64:ff9b::/96",
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

func mustParsePrefixes(raw []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// IsUnsafeAddr reports whether addr is invalid, has an IPv6 zone identifier, or
// belongs to any non-public, loopback, private, CGNAT, transition, link-local,
// multicast, or reserved address range.
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
	for _, pfx := range unsafePrefixes {
		if pfx.Contains(addr) {
			return true
		}
	}
	return false
}

// IsUnsafeIP reports whether ip is nil or not globally routable.
func IsUnsafeIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return IsUnsafeAddr(addr)
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
		return IsUnsafeAddr(addr)
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
