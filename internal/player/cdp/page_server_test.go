package cdp

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testPage = "<html>dev-token-only</html>"

// get issues a GET to rawURL with the given Host header ("" keeps the URL's).
func get(t *testing.T, method, rawURL, host string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPageServer_ServesPageOnPrefix(t *testing.T) {
	s, err := newPageServer(testPage)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	code, body := get(t, http.MethodGet, s.pageURL(), "")
	if code != http.StatusOK || body != testPage {
		t.Fatalf("GET %s = %d %q, want 200 %q", s.pageURL(), code, body, testPage)
	}
}

func TestPageServer_RejectsOtherPathsAndMethods(t *testing.T) {
	s, err := newPageServer(testPage)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	base := "http://" + s.host
	for _, target := range []string{
		base + "/",
		base + "/index.html",
		base + "/wrong-prefix/",
		base + s.prefix + "extra",
		base + strings.TrimSuffix(s.prefix, "/"),
	} {
		if code, body := get(t, http.MethodGet, target, ""); code != http.StatusNotFound || strings.Contains(body, "dev-token") {
			t.Errorf("GET %s = %d %q, want 404 with no page", target, code, body)
		}
	}
	if code, _ := get(t, http.MethodPost, s.pageURL(), ""); code != http.StatusNotFound {
		t.Errorf("POST page = %d, want 404", code)
	}
}

// A page elsewhere that resolves its own name to 127.0.0.1 (DNS rebinding)
// sends that name as Host, and must not be served even with the right path.
func TestPageServer_RejectsForeignHost(t *testing.T) {
	s, err := newPageServer(testPage)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	for _, host := range []string{"rebind-test.example", "localhost", "127.0.0.1"} {
		if code, body := get(t, http.MethodGet, s.pageURL(), host); code != http.StatusNotFound || body == testPage {
			t.Errorf("Host %q = %d %q, want 404", host, code, body)
		}
	}
}

func TestPageServer_PrefixIsPerSession(t *testing.T) {
	a, err := newPageServer(testPage)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	b, err := newPageServer(testPage)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	if a.prefix == b.prefix {
		t.Fatalf("two servers share the prefix %q", a.prefix)
	}
	if len(a.prefix) < 20 {
		t.Errorf("prefix %q is too short to be unguessable", a.prefix)
	}
}
