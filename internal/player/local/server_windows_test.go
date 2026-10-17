//go:build windows

package local

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider"
)

func startMediaServer(t *testing.T) *mediaServer {
	t.Helper()
	m, err := newMediaServer()
	if err != nil {
		t.Fatalf("newMediaServer: %v", err)
	}
	t.Cleanup(m.close)
	return m
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, rawURL string, header map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil) //nolint:gosec // G107: loopback test server URL
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

// audioBytes is distinct at every offset so a wrong range cannot pass.
func audioBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func TestMediaServerListensOnLoopbackOnly(t *testing.T) {
	m := startMediaServer(t)
	host, _, err := net.SplitHostPort(m.host)
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("listening on %s, want a loopback address", m.host)
	}
	resp, body := get(t, m.pageURL(), nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`src="engine.js"`)) {
		t.Fatalf("page: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, m.pageURL()+"engine.js", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("vibezLocalEngine")) {
		t.Fatalf("engine.js: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("engine.js Content-Type %q", ct)
	}
}

// Library paths routinely hold spaces, '#', and non-ASCII, none of which may
// break or leak into the URL Chrome fetches.
func TestMediaServerServesRegisteredFilesWithAwkwardNames(t *testing.T) {
	m := startMediaServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "Sigur Rós #1 — Hoppípolla.flac")
	data := audioBytes(4096)
	writeFile(t, path, data)
	id := "local:" + path
	m.setTracks([]provider.Track{{ID: id}})

	u, ok := m.url(id)
	if !ok {
		t.Fatal("registered track has no URL")
	}
	if strings.ContainsAny(u, " #") || strings.Contains(u, "Sigur") {
		t.Fatalf("URL %q exposes the file name", u)
	}
	resp, body := get(t, u, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, data) {
		t.Fatalf("GET: %d, %d bytes, want 200 and the file", resp.StatusCode, len(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/flac" {
		t.Fatalf("Content-Type %q, want audio/flac", ct)
	}
}

// Chrome seeks, and reads container indexes at the end of a file, with
// byte ranges.
func TestMediaServerAnswersByteRanges(t *testing.T) {
	m := startMediaServer(t)
	path := filepath.Join(t.TempDir(), "a.mp3")
	data := audioBytes(10000)
	writeFile(t, path, data)
	m.setTracks([]provider.Track{{ID: "local:" + path}})
	u, _ := m.url("local:" + path)

	resp, body := get(t, u, map[string]string{"Range": "bytes=1000-1999"})
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, data[1000:2000]) {
		t.Fatalf("range: %d, %d bytes", resp.StatusCode, len(body))
	}
	if cr := resp.Header.Get("Content-Range"); cr != "bytes 1000-1999/10000" {
		t.Fatalf("Content-Range %q", cr)
	}
	resp, body = get(t, u, map[string]string{"Range": "bytes=-100"})
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, data[9900:]) {
		t.Fatalf("suffix range: %d, %d bytes", resp.StatusCode, len(body))
	}
}

// Nothing but a registered track's route reaches the filesystem: not a file
// beside it, not a path smuggled into the URL, not the right route under a
// guessed prefix or through another Host.
func TestMediaServerExposesNothingElse(t *testing.T) {
	m := startMediaServer(t)
	dir := t.TempDir()
	track := filepath.Join(dir, "a.mp3")
	secret := filepath.Join(dir, "secret.txt")
	writeFile(t, track, audioBytes(64))
	writeFile(t, secret, []byte("do not serve"))
	m.setTracks([]provider.Track{{ID: "local:" + track}})
	good, _ := m.url("local:" + track)
	route := good[strings.LastIndex(good, "/")+1:]
	base := "http://" + m.host

	for _, u := range []string{
		m.pageURL() + "media/secret.txt",
		m.pageURL() + "media/" + url.PathEscape(secret),
		m.pageURL() + "media/" + url.PathEscape("local:"+secret),
		m.pageURL() + "media/..%2Fsecret.txt",
		m.pageURL() + "media/" + route + "/../../secret.txt",
		base + "/" + url.PathEscape(secret),
		base + "/media/" + route,
		base + "/WRONGTOKEN/media/" + route,
		base + "/",
	} {
		resp, body := get(t, u, nil)
		if resp.StatusCode == http.StatusOK || bytes.Contains(body, []byte("do not serve")) {
			t.Errorf("GET %s: %d %q", u, resp.StatusCode, body)
		}
	}

	resp, _ := get(t, good, map[string]string{"Host": "vibez.attacker.example"})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("foreign Host: %d, want 404", resp.StatusCode)
	}
	post, err := http.Post(good, "text/plain", strings.NewReader("x")) //nolint:gosec // G107: loopback test server URL
	if err != nil {
		t.Fatal(err)
	}
	_ = post.Body.Close()
	if post.StatusCode == http.StatusOK {
		t.Error("POST to a media route succeeded")
	}
}

// A rescan keeps the route of a file that is still in the library, so a
// track mid-stream stays reachable, and drops the route of one that is not.
func TestMediaServerRescan(t *testing.T) {
	m := startMediaServer(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.ogg"), filepath.Join(dir, "b.ogg")
	writeFile(t, a, audioBytes(32))
	writeFile(t, b, audioBytes(32))
	m.setTracks([]provider.Track{{ID: "local:" + a}, {ID: "local:" + b}})
	urlA, _ := m.url("local:" + a)
	urlB, _ := m.url("local:" + b)

	m.setTracks([]provider.Track{{ID: "local:" + a}})
	if again, _ := m.url("local:" + a); again != urlA {
		t.Fatalf("route of a kept track changed: %s → %s", urlA, again)
	}
	if _, ok := m.url("local:" + b); ok {
		t.Fatal("dropped track still has a URL")
	}
	if resp, _ := get(t, urlB, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("dropped track's old route: %d, want 404", resp.StatusCode)
	}
	if resp, _ := get(t, urlA, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("kept track: %d", resp.StatusCode)
	}
}

func TestMediaServerStopsOnClose(t *testing.T) {
	m, err := newMediaServer()
	if err != nil {
		t.Fatal(err)
	}
	m.close()
	if resp, err := http.Get(m.pageURL()); err == nil { //nolint:gosec // G107: loopback test server URL
		_ = resp.Body.Close()
		t.Fatal("server still answering after close")
	}
}
