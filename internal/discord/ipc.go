package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Discord IPC Opcodes
const (
	OpHandshake uint32 = 0
	OpFrame     uint32 = 1
	OpClose     uint32 = 2
	OpPing      uint32 = 3
	OpPong      uint32 = 4
)

const (
	defaultTimeout = 3 * time.Second

	// ipcEndpointCount is the number of discord-ipc-N endpoints (N = 0..9) Discord may listen on.
	ipcEndpointCount = 10
)

var (
	ErrClosed = errors.New("discord ipc connection closed")
)

// IPCClient handles communication with the local Discord client over its IPC endpoint
// (a Unix domain socket, or a named pipe on Windows).
type IPCClient struct {
	conn    net.Conn
	mu      sync.Mutex
	onFrame func(op uint32, payload []byte)
}

// DialIPC locates and connects to an active Discord IPC endpoint.
func DialIPC() (*IPCClient, error) {
	for _, p := range candidateIPCPaths() {
		conn, err := dialIPCPath(p)
		if err == nil {
			return &IPCClient{conn: conn}, nil
		}
	}
	return nil, errors.New("no active discord ipc endpoint found")
}

// Handshake sends the handshake frame, reads the ready response, and starts background reader.
func (c *IPCClient) Handshake(clientID string) error {
	req := map[string]any{
		"v":         1,
		"client_id": clientID,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := c.WriteFrame(OpHandshake, data); err != nil {
		return err
	}

	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.SetReadDeadline(time.Now().Add(defaultTimeout))
	}
	c.mu.Unlock()

	op, resp, err := c.ReadFrame()

	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.SetReadDeadline(time.Time{})
	}
	c.mu.Unlock()

	if err != nil {
		return err
	}
	if op == OpClose {
		return fmt.Errorf("handshake rejected: %s", string(resp))
	}
	if op != OpFrame {
		return fmt.Errorf("unexpected opcode %d: %s", op, string(resp))
	}

	c.startReader()
	return nil
}

// SetOnFrame registers a callback for frames received by the background reader.
func (c *IPCClient) SetOnFrame(fn func(op uint32, payload []byte)) {
	c.mu.Lock()
	c.onFrame = fn
	c.mu.Unlock()
}

// startReader drains incoming Discord frames and responds to Pings.
func (c *IPCClient) startReader() {
	go func() {
		for {
			op, payload, err := c.ReadFrame()
			if err != nil {
				_ = c.Close()
				return
			}
			if op == OpPing {
				_ = c.WriteFrame(OpPong, payload)
			} else {
				c.mu.Lock()
				fn := c.onFrame
				c.mu.Unlock()
				if fn != nil {
					fn(op, payload)
				}
			}
		}
	}()
}

// WriteFrame sends an opcode and payload prefixed with an 8-byte little-endian header.
func (c *IPCClient) WriteFrame(op uint32, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return ErrClosed
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(defaultTimeout))
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], op)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(payload))) //nolint:gosec // payload length fits in uint32

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadFrame reads an 8-byte little-endian header and payload from the IPC connection.
func (c *IPCClient) ReadFrame() (uint32, []byte, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return 0, nil, ErrClosed
	}
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(conn, payload); err != nil {
			return 0, nil, err
		}
	}
	return op, payload, nil
}

// Close closes the underlying IPC connection.
func (c *IPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}
