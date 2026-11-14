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

func TestSocketDirs_OnlyAbsolutePathsWhenXDGUnset(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "relative-tmp")
	t.Setenv("TMP", "")
	t.Setenv("TEMP", "")
	for _, d := range socketDirs() {
		if !filepath.IsAbs(d) {
			t.Errorf("socketDirs() returned relative directory %q", d)
		}
	}
}

func TestDialIPCPath_RejectsNonSocketAndSymlink(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "discord-ipc-0")
	if err := os.WriteFile(reg, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}
	if conn, err := dialIPCPath(reg); err == nil {
		_ = conn.Close()
		t.Fatal("dialIPCPath succeeded on regular file, expected error")
	}

	l, sockPath := listenTestIPC(t)
	defer func() { _ = l.Close() }()
	link := filepath.Join(dir, "discord-ipc-1")
	if err := os.Symlink(sockPath, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if conn, err := dialIPCPath(link); err == nil {
		_ = conn.Close()
		t.Fatal("dialIPCPath followed symlink, expected error")
	}
}
