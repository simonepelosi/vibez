package netguard

import (
	"net"
	"net/netip"
	"testing"
)

func TestIsUnsafeAddr_CoversSpecialUseAndTransitionRanges(t *testing.T) {
	unsafe := []string{
		"0.0.0.0",
		"0.1.2.3",
		"10.0.0.1",
		"100.64.0.1",
		"100.100.100.200",
		"127.0.0.1",
		"169.254.169.254",
		"172.16.0.1",
		"192.0.0.1",
		"192.0.2.1",
		"192.88.99.1",
		"192.168.1.1",
		"198.51.100.1",
		"203.0.113.1",
		"224.0.0.1",
		"240.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"::1%lo",
		"::127.0.0.1",
		"::ffff:127.0.0.1",
		"::ffff:100.64.0.1",
		"64:ff9b::127.0.0.1",
		"64:ff9b:1::10.0.0.1",
		"100::1",
		"2001::1",
		"2001:2::1",
		"2001:db8::1",
		"2002:7f00:1::1",
		"fc00::1",
		"fe80::1",
		"fe80::1%eth0",
		"fec0::1",
		"ff02::1",
	}
	for _, s := range unsafe {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("ParseAddr(%q) failed: %v", s, err)
		}
		if !IsUnsafeAddr(addr) {
			t.Errorf("IsUnsafeAddr(%q) = false, want true", s)
		}
	}

	safe := []string{"93.184.216.34", "17.253.144.10", "2606:2800:220:1:248:1893:25c8:1946"}
	for _, s := range safe {
		addr := netip.MustParseAddr(s)
		if IsUnsafeAddr(addr) {
			t.Errorf("IsUnsafeAddr(%q) = true, want false", s)
		}
	}
	if !IsUnsafeIP(nil) || IsUnsafeIP(net.ParseIP("93.184.216.34")) {
		t.Fatal("IsUnsafeIP returned unexpected result")
	}
}

func TestParseSafeURL_BlocksBypassForms(t *testing.T) {
	bad := []string{
		"",
		"http://is1-ssl.mzstatic.com/art.jpg",
		"https://user:pass@is1-ssl.mzstatic.com/art.jpg",
		"https://localhost/art.jpg",
		"https://sub.localhost/art.jpg",
		"https://service.local/art.jpg",
		"https://metadata.internal/art.jpg",
		"https://[::1%25lo]/art.jpg",
		"https://[fe80::1%25eth0]/art.jpg",
		"https://2130706433/art.jpg",
		"https://0x7f000001/art.jpg",
		"https://0177.0.0.1/art.jpg",
		"https://127.1/art.jpg",
		"https://100.64.0.1/art.jpg",
		"https://[64:ff9b::127.0.0.1]/art.jpg",
		"https://is1-ssl.mzstatic.com/\u009bart.jpg",
		"https://is1-ssl.mzstatic.com/\u202eart.jpg",
		"http://127.0.0.1:8080/art.jpg",
	}
	for _, raw := range bad {
		if _, err := ParseSafeURL(raw, 2048, false); err == nil {
			t.Errorf("ParseSafeURL(%q, allowLoopback=false) = nil, want error", raw)
		}
	}

	if _, err := ParseSafeURL("https://is1-ssl.mzstatic.com/image/thumb/1/512x512bb.jpg", 2048, false); err != nil {
		t.Errorf("ParseSafeURL(Apple CDN) failed: %v", err)
	}
	if _, err := ParseSafeURL("http://127.0.0.1:8080/art.jpg", 2048, true); err != nil {
		t.Errorf("ParseSafeURL(loopback with allowLoopback=true) failed: %v", err)
	}
	if _, err := ParseSafeURL("http://[::1%25lo]:8080/art.jpg", 2048, true); err == nil {
		t.Error("ParseSafeURL(loopback zone ID) unexpectedly succeeded")
	}
}

func TestSafeDialControl_BlocksUnsafeTargets(t *testing.T) {
	strict := SafeDialControl(false)
	for _, addr := range []string{"127.0.0.1:80", "100.64.0.1:443", "0.1.2.3:80", "[::1%lo0]:80", "[64:ff9b::127.0.0.1]:443"} {
		if err := strict("tcp", addr, nil); err == nil {
			t.Errorf("SafeDialControl(false)(%q) = nil, want error", addr)
		}
	}
	if err := strict("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("SafeDialControl(false)(public) = %v, want nil", err)
	}

	withLoop := SafeDialControl(true)
	if err := withLoop("tcp", "127.0.0.1:8080", nil); err != nil {
		t.Errorf("SafeDialControl(true)(loopback) = %v, want nil", err)
	}
	if err := withLoop("tcp", "100.64.0.1:8080", nil); err == nil {
		t.Error("SafeDialControl(true)(100.64.0.1) = nil, want error")
	}
}
