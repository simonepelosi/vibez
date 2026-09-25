package discord

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
)

// DefaultClientID is the registered Discord Application ID for vibez.
const DefaultClientID = "1553056382482129088"

// reconnectCooldown is the time to wait before retrying to connect after a failure.
const reconnectCooldown = 15 * time.Second

// Service maintains the Discord Rich Presence session.
type Service struct {
	clientID string
	client   *IPCClient
	logMu    sync.RWMutex
	logger   func(string)
	mu       sync.Mutex

	nonceCounter atomic.Uint64
	lastActivity *Activity
	lastTrackID  string
	lastPlaying  bool
	lastConnErr  time.Time
	closed       bool
}

// NewService creates a Discord Rich Presence service.
func NewService(clientID string) *Service {
	if clientID == "" {
		clientID = DefaultClientID
	}
	return &Service{
		clientID: clientID,
	}
}

// SetLogger registers a logging callback.
func (s *Service) SetLogger(fn func(string)) {
	s.logMu.Lock()
	s.logger = fn
	s.logMu.Unlock()
}

func (s *Service) log(msg string) {
	s.logMu.RLock()
	fn := s.logger
	s.logMu.RUnlock()
	if fn != nil {
		fn("[discord] " + msg)
	}
}

type setActivityArgs struct {
	Pid      int       `json:"pid"`
	Activity *Activity `json:"activity"`
}

type setActivityCmd struct {
	Cmd   string          `json:"cmd"`
	Args  setActivityArgs `json:"args"`
	Nonce string          `json:"nonce"`
}

// Update updates the Rich Presence based on the player's current state.
func (s *Service) Update(st player.State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	trackID := ""
	if st.Track != nil {
		trackID = st.Track.ID
	}

	// Avoid re-sending identical presence if track and playing status haven't changed
	if trackID == s.lastTrackID && st.Playing == s.lastPlaying && s.client != nil {
		return
	}

	act := BuildActivity(st)

	// Ensure connection
	if err := s.ensureConnected(); err != nil {
		return
	}

	nonce := fmt.Sprintf("%d", s.nonceCounter.Add(1))
	cmd := setActivityCmd{
		Cmd: "SET_ACTIVITY",
		Args: setActivityArgs{
			Pid:      os.Getpid(),
			Activity: act,
		},
		Nonce: nonce,
	}

	payload, err := json.Marshal(cmd)
	if err != nil {
		s.log(fmt.Sprintf("marshal error: %v", err))
		return
	}

	if err := s.client.WriteFrame(OpFrame, payload); err != nil {
		s.log(fmt.Sprintf("write error: %v", err))
		_ = s.client.Close()
		s.client = nil
		s.lastConnErr = time.Now()
		return
	}

	s.lastActivity = act
	s.lastTrackID = trackID
	s.lastPlaying = st.Playing

	if act != nil {
		s.log(fmt.Sprintf("presence updated: %s — %s (%s)", act.State, act.Details, act.Assets.SmallText))
	} else {
		s.log("presence cleared")
	}
}

// ensureConnected verifies or establishes an IPC connection to Discord.
// Must be called with s.mu held.
func (s *Service) ensureConnected() error {
	if s.client != nil {
		return nil
	}

	if time.Since(s.lastConnErr) < reconnectCooldown {
		return ErrClosed
	}

	ipc, err := DialIPC()
	if err != nil {
		s.lastConnErr = time.Now()
		return err
	}

	if err := ipc.Handshake(s.clientID); err != nil {
		_ = ipc.Close()
		s.lastConnErr = time.Now()
		s.log(fmt.Sprintf("handshake failed: %v", err))
		return err
	}

	s.client = ipc
	s.log("connected to Discord IPC")
	return nil
}

// Clear clears the current Discord activity.
func (s *Service) Clear() {
	s.Update(player.State{})
}

// Close closes the Discord IPC connection.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	if s.client != nil {
		// Attempt to clear activity before disconnecting
		nonce := fmt.Sprintf("%d", s.nonceCounter.Add(1))
		cmd := setActivityCmd{
			Cmd: "SET_ACTIVITY",
			Args: setActivityArgs{
				Pid:      os.Getpid(),
				Activity: nil,
			},
			Nonce: nonce,
		}
		if payload, err := json.Marshal(cmd); err == nil {
			_ = s.client.WriteFrame(OpFrame, payload)
		}
		err := s.client.Close()
		s.client = nil
		return err
	}
	return nil
}

// Start launches a goroutine watching player state updates and synchronizing with Discord.
func Start(clientID string, p interface{ Subscribe() <-chan player.State }, log func(string)) *Service {
	s := NewService(clientID)
	if log != nil {
		s.SetLogger(log)
	}
	go func() {
		for st := range p.Subscribe() {
			s.Update(st)
		}
		_ = s.Close()
	}()
	return s
}
