//go:build !windows

package discord

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// shortTempSocketPath returns a short temporary unix socket path that fits within Darwin's 104-byte sun_path limit.
func shortTempSocketPath(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("/tmp", "d-*.sock")
	if err != nil {
		f, err = os.CreateTemp("", "d-*.sock")
		if err != nil {
			t.Fatalf("failed to create temp socket path: %v", err)
		}
	}
	p := f.Name()
	_ = f.Close()
	_ = os.Remove(p)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

// listenTestIPC listens on a fresh unix socket and returns the listener and its dial path.
func listenTestIPC(t *testing.T) (net.Listener, string) {
	t.Helper()
	path := shortTempSocketPath(t)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}

func TestDialIPC_FindsSocketInXDGRuntimeDir(t *testing.T) {
	// Keep the directory short so the socket path fits Darwin's sun_path limit.
	dir, err := os.MkdirTemp("/tmp", "d-")
	if err != nil {
		dir, err = os.MkdirTemp("", "d-")
		if err != nil {
			t.Fatalf("failed to create runtime dir: %v", err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)

	l, err := net.Listen("unix", filepath.Join(dir, "discord-ipc-0"))
	if err != nil {
		t.Fatalf("failed to listen on discord-ipc-0: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	frames := serveMockDiscord(l)

	client, err := DialIPC()
	if err != nil {
		t.Fatalf("DialIPC: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Handshake(DefaultClientID); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}
	expectHandshake(t, frames)
}
