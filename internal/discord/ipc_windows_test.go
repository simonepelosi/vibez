//go:build windows

package discord

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

var testPipeSeq atomic.Uint64

// listenTestPipe listens on the named pipe at path. The pipe is buffered like
// Discord's own IPC pipes, so a write completes without waiting for the peer to read.
func listenTestPipe(t *testing.T, path string) net.Listener {
	t.Helper()
	l, err := winio.ListenPipe(path, &winio.PipeConfig{
		InputBufferSize:  64 << 10,
		OutputBufferSize: 64 << 10,
	})
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", path, err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// listenTestIPC listens on a fresh, uniquely named pipe and returns the listener and its dial path.
func listenTestIPC(t *testing.T) (net.Listener, string) {
	t.Helper()
	path := fmt.Sprintf(`\\.\pipe\vibez-discord-test-%d-%d`, os.Getpid(), testPipeSeq.Add(1))
	return listenTestPipe(t, path), path
}

func TestDialIPC_FindsDiscordNamedPipe(t *testing.T) {
	const path = `\\.\pipe\discord-ipc-0`
	if conn, err := dialIPCPath(path); err == nil {
		_ = conn.Close()
		t.Skipf("%s is served by a running Discord client", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s is already in use: %v", path, err)
	}

	frames := serveMockDiscord(listenTestPipe(t, path))

	client, err := DialIPC()
	if err != nil {
		t.Fatalf("DialIPC: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Handshake(DefaultClientID); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}
	expectHandshake(t, frames)

	// The mock server closes its pipe instance once it sees the disconnect, which
	// frees discord-ipc-0 again before the next test probes for Discord.
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("mock Discord server did not observe the client disconnect")
		}
	}
}

func TestDialIPC_WithoutDiscordFailsFast(t *testing.T) {
	start := time.Now()
	client, err := DialIPC()
	elapsed := time.Since(start)
	if err == nil {
		_ = client.Close()
		t.Skip("a Discord IPC pipe is being served on this machine")
	}
	if elapsed >= defaultTimeout {
		t.Fatalf("DialIPC took %v to report no Discord pipe, want well under %v", elapsed, defaultTimeout)
	}
}
