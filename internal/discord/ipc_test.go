package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// readTestFrame reads one Discord IPC frame from the peer side of a test connection.
func readTestFrame(r io.Reader) (uint32, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return op, payload, nil
}

// writeTestFrame writes one Discord IPC frame from the peer side of a test connection.
func writeTestFrame(w io.Writer, op uint32, payload []byte) error {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], op)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(payload))) //nolint:gosec
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// dialTestIPC connects to a test IPC endpoint with the platform's production dialer.
func dialTestIPC(t *testing.T, path string) net.Conn {
	t.Helper()
	conn, err := dialIPCPath(path)
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// mockDiscordServer creates a fake Discord IPC listener on the platform's IPC transport.
func mockDiscordServer(t *testing.T) (string, chan []byte) {
	t.Helper()
	l, path := listenTestIPC(t)
	return path, serveMockDiscord(l)
}

// serveMockDiscord accepts one connection on l and answers it like the Discord client,
// publishing every received payload on the returned channel.
func serveMockDiscord(l net.Listener) chan []byte {
	frames := make(chan []byte, 10)

	go func() {
		defer close(frames)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			op, payload, err := readTestFrame(conn)
			if err != nil {
				return
			}

			frames <- payload

			// Respond to handshake or frames
			switch op {
			case OpHandshake:
				_ = writeTestFrame(conn, OpFrame, []byte(`{"cmd":"DISPATCH","evt":"READY","data":{"v":1}}`))
			case OpFrame:
				_ = writeTestFrame(conn, OpFrame, []byte(`{"cmd":"SET_ACTIVITY","data":{},"evt":null}`))
			}
		}
	}()

	return frames
}

// expectHandshake asserts that the next payload the mock server received is a handshake for DefaultClientID.
func expectHandshake(t *testing.T, frames <-chan []byte) {
	t.Helper()
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

func TestIPCClient_Handshake(t *testing.T) {
	path, frames := mockDiscordServer(t)

	client := &IPCClient{conn: dialTestIPC(t, path)}
	if err := client.Handshake(DefaultClientID); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	expectHandshake(t, frames)
}

func TestIPCClient_PingPongAndPeerClose(t *testing.T) {
	l, path := listenTestIPC(t)

	pongs := make(chan []byte, 1)
	serverErr := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.Close() }()

		if op, _, err := readTestFrame(conn); err != nil || op != OpHandshake {
			serverErr <- fmt.Errorf("handshake read: op=%d err=%v", op, err)
			return
		}
		if err := writeTestFrame(conn, OpFrame, []byte(`{"cmd":"DISPATCH","evt":"READY"}`)); err != nil {
			serverErr <- err
			return
		}
		if err := writeTestFrame(conn, OpPing, []byte(`{"nonce":"ping-1"}`)); err != nil {
			serverErr <- err
			return
		}
		op, payload, err := readTestFrame(conn)
		if err != nil {
			serverErr <- err
			return
		}
		if op != OpPong {
			serverErr <- fmt.Errorf("reply opcode = %d, want OpPong", op)
			return
		}
		pongs <- payload
		// Returning closes the server end, as Discord does when it shuts down.
	}()

	client := &IPCClient{conn: dialTestIPC(t, path)}
	if err := client.Handshake(DefaultClientID); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	select {
	case payload := <-pongs:
		if string(payload) != `{"nonce":"ping-1"}` {
			t.Fatalf("pong payload = %q, want the ping payload echoed", payload)
		}
	case err := <-serverErr:
		t.Fatalf("mock server: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pong")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(client.WriteFrame(OpFrame, []byte(`{}`)), ErrClosed) {
		if time.Now().After(deadline) {
			t.Fatal("client stayed open after the peer closed the connection")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestIPCClient_CloseDisconnectsPeer(t *testing.T) {
	l, path := listenTestIPC(t)

	peerReadErr := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			peerReadErr <- err
			return
		}
		defer func() { _ = conn.Close() }()

		if _, _, err := readTestFrame(conn); err != nil {
			peerReadErr <- err
			return
		}
		if err := writeTestFrame(conn, OpFrame, []byte(`{"cmd":"DISPATCH","evt":"READY"}`)); err != nil {
			peerReadErr <- err
			return
		}
		_, _, err = readTestFrame(conn)
		peerReadErr <- err
	}()

	client := &IPCClient{conn: dialTestIPC(t, path)}
	if err := client.Handshake(DefaultClientID); err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	// The background reader is now blocked on the connection; Close must still return promptly.
	closed := make(chan error, 1)
	go func() { closed <- client.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked while the background reader was active")
	}

	select {
	case err := <-peerReadErr:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("peer read after client Close = %v, want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer did not observe the client closing the connection")
	}

	if err := client.WriteFrame(OpFrame, []byte(`{}`)); !errors.Is(err, ErrClosed) {
		t.Fatalf("WriteFrame after Close = %v, want ErrClosed", err)
	}
}

func TestService_EndToEnd(t *testing.T) {
	path, frames := mockDiscordServer(t)

	svc := NewService(DefaultClientID)
	svc.client = &IPCClient{conn: dialTestIPC(t, path)}

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
	path, frames := mockDiscordServer(t)

	svc := NewService(DefaultClientID)
	svc.client = &IPCClient{conn: dialTestIPC(t, path)}
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
	l, path := listenTestIPC(t)

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

	client := &IPCClient{conn: dialTestIPC(t, path)}
	start := time.Now()
	err := client.Handshake(DefaultClientID)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Handshake to fail due to timeout, but it succeeded")
	}
	if elapsed > defaultTimeout+2*time.Second {
		t.Fatalf("Handshake took too long to time out: %v", elapsed)
	}
}
