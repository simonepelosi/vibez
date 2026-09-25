package discord

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// mockDiscordServer creates a fake Discord IPC listener on a unix socket.
func mockDiscordServer(t *testing.T) (string, chan []byte, func()) {
	t.Helper()
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "discord-ipc-0")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on mock socket: %v", err)
	}

	frames := make(chan []byte, 10)
	done := make(chan struct{})

	go func() {
		defer close(frames)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			header := make([]byte, 8)
			if _, err := io.ReadFull(conn, header); err != nil {
				return
			}
			op := binary.LittleEndian.Uint32(header[0:4])
			length := binary.LittleEndian.Uint32(header[4:8])
			payload := make([]byte, length)
			if length > 0 {
				if _, err := io.ReadFull(conn, payload); err != nil {
					return
				}
			}

			frames <- payload

			// Respond to handshake or frames
			switch op {
			case OpHandshake:
				resp := []byte(`{"cmd":"DISPATCH","evt":"READY","data":{"v":1}}`)
				respHeader := make([]byte, 8)
				binary.LittleEndian.PutUint32(respHeader[0:4], OpFrame)
				binary.LittleEndian.PutUint32(respHeader[4:8], uint32(len(resp))) //nolint:gosec
				_, _ = conn.Write(respHeader)
				_, _ = conn.Write(resp)
			case OpFrame:
				resp := []byte(`{"cmd":"SET_ACTIVITY","data":{},"evt":null}`)
				respHeader := make([]byte, 8)
				binary.LittleEndian.PutUint32(respHeader[0:4], OpFrame)
				binary.LittleEndian.PutUint32(respHeader[4:8], uint32(len(resp))) //nolint:gosec
				_, _ = conn.Write(respHeader)
				_, _ = conn.Write(resp)
			}
		}
	}()

	cleanup := func() {
		close(done)
		_ = l.Close()
	}

	return sockPath, frames, cleanup
}

func TestIPCClient_Handshake(t *testing.T) {
	sockPath, frames, cleanup := mockDiscordServer(t)
	defer cleanup()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := &IPCClient{conn: conn}
	if err := client.Handshake(DefaultClientID); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	select {
	case payload := <-frames:
		var req map[string]any
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("invalid handshake payload: %v", err)
		}
		if req["client_id"] != DefaultClientID {
			t.Errorf("client_id = %v, want %v", req["client_id"], DefaultClientID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handshake frame on server")
	}
}

func TestService_EndToEnd(t *testing.T) {
	sockPath, frames, cleanup := mockDiscordServer(t)
	defer cleanup()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}

	svc := NewService(DefaultClientID)
	svc.client = &IPCClient{conn: conn}

	var logs []string
	svc.SetLogger(func(msg string) {
		logs = append(logs, msg)
	})

	// Send playing state
	svc.Update(player.State{
		Track: &provider.Track{
			ID:     "track-1",
			Title:  "Redbone",
			Artist: "Childish Gambino",
			Album:  "Awaken, My Love!",
		},
		Playing: true,
	})

	select {
	case payload := <-frames:
		var cmd setActivityCmd
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Fatalf("failed to unmarshal setActivityCmd: %v", err)
		}
		if cmd.Cmd != "SET_ACTIVITY" {
			t.Errorf("cmd = %q, want SET_ACTIVITY", cmd.Cmd)
		}
		if cmd.Args.Activity == nil || cmd.Args.Activity.Details != "Redbone" {
			t.Errorf("activity details = %v, want Redbone", cmd.Args.Activity)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for activity frame")
	}

	// Close service should clear activity
	_ = svc.Close()
	select {
	case payload := <-frames:
		var cmd setActivityCmd
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Fatalf("failed to unmarshal clear activity cmd: %v", err)
		}
		if cmd.Args.Activity != nil {
			t.Errorf("activity on close = %+v, want nil (cleared)", cmd.Args.Activity)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for clear activity frame on close")
	}
}

func TestService_SeekAndRepeatOneDrift(t *testing.T) {
	sockPath, frames, cleanup := mockDiscordServer(t)
	defer cleanup()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}

	svc := NewService(DefaultClientID)
	svc.client = &IPCClient{conn: conn}
	defer func() { _ = svc.Close() }()

	tr := &provider.Track{
		ID:       "track-seek",
		Title:    "Sunflower",
		Artist:   "Post Malone",
		Duration: 200 * time.Second,
	}

	// 1. Initial play at position 10s
	svc.Update(player.State{
		Track:    tr,
		Playing:  true,
		Position: 10 * time.Second,
	})

	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial frame")
	}

	// 2. Normal playback tick (position 11s, ~1s diff) -> should be deduplicated (no frame)
	svc.Update(player.State{
		Track:    tr,
		Playing:  true,
		Position: 11 * time.Second,
	})

	select {
	case <-frames:
		t.Fatal("expected frame to be deduplicated, but received one")
	case <-time.After(100 * time.Millisecond):
		// Expected deduplication
	}

	// 3. User seeks to position 80s (drift > 2s) -> should send new activity
	svc.Update(player.State{
		Track:    tr,
		Playing:  true,
		Position: 80 * time.Second,
	})

	select {
	case payload := <-frames:
		var cmd setActivityCmd
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if cmd.Args.Activity == nil || cmd.Args.Activity.Timestamps == nil {
			t.Fatal("expected non-nil timestamps after seek")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for seek frame")
	}

	// 4. Repeat-one loops back to position 0s -> should send new activity
	svc.Update(player.State{
		Track:    tr,
		Playing:  true,
		Position: 0 * time.Second,
	})

	select {
	case payload := <-frames:
		var cmd setActivityCmd
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if cmd.Args.Activity == nil || cmd.Args.Activity.Timestamps == nil {
			t.Fatal("expected non-nil timestamps after loop")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for loop frame")
	}
}

func TestIPCClient_HandshakeTimeout(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "silent-socket")
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer func() { _ = l.Close() }()

	// Accept connection but do nothing (silent / hung server)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			// Read handshake to keep buffer clear, but never reply
			buf := make([]byte, 1024)
			_, _ = conn.Read(buf)
			// keep conn open until listener is closed
			<-time.After(2 * defaultTimeout)
			_ = conn.Close()
		}
	}()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := &IPCClient{conn: conn}
	start := time.Now()
	err = client.Handshake(DefaultClientID)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Handshake to fail due to timeout, but it succeeded")
	}
	if elapsed > defaultTimeout+2*time.Second {
		t.Fatalf("Handshake took too long to time out: %v", elapsed)
	}
}
