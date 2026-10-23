// Package smtc publishes vibez's playback to the Windows System Media
// Transport Controls (SMTC). SMTC is what the keyboard's media keys, the volume
// flyout and status-bar media widgets such as YASB read and control, so with a
// Server running they show the current track and drive the player.
//
// Usage mirrors the MPRIS server on Linux:
//
//	srv, err := smtc.NewServer(audioEngine, logf)
//	if err != nil { /* SMTC unavailable: non-fatal */ }
//	defer srv.Close()
//	for st := range audioEngine.Subscribe() { srv.Update(st) }
//
// The native side exists only on Windows with cgo. Elsewhere NewServer returns
// ErrUnavailable.
package smtc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
)

// ErrUnavailable is returned by NewServer when this build has no SMTC bridge:
// any OS other than Windows, or Windows without cgo.
var ErrUnavailable = errors.New("smtc: not available in this build")

// Controller is the subset of the player that SMTC buttons drive.
type Controller interface {
	Play() error
	Pause() error
	Next() error
	Previous() error
	Seek(time.Duration) error
}

// eventKind is a request from SMTC: a button press or a seek.
type eventKind int

const (
	eventPlay eventKind = iota + 1
	eventPause
	eventStop
	eventNext
	eventPrevious
	eventSeek
)

func (k eventKind) String() string {
	switch k {
	case eventPlay:
		return "play"
	case eventPause:
		return "pause"
	case eventStop:
		return "stop"
	case eventNext:
		return "next"
	case eventPrevious:
		return "previous"
	case eventSeek:
		return "seek"
	}
	return fmt.Sprintf("event(%d)", int(k))
}

type event struct {
	kind     eventKind
	position time.Duration // eventSeek only
}

// bridge is the native side. apply must not block: it is called from the
// player's Subscribe loop. close must stop event delivery before it returns.
type bridge interface {
	apply(u update)
	close()
}

// bridgeFactory creates a bridge that reports SMTC requests to onEvent and
// native failures to onError. Both callbacks may run on any thread and must
// not block.
type bridgeFactory func(onEvent func(event), onError func(string)) (bridge, error)

const (
	// eventQueueSize bounds the requests waiting for the controller. A press
	// that arrives while the queue is full is dropped rather than blocking
	// Windows.
	eventQueueSize = 16

	// logQueueSize bounds the messages waiting for logf, which may block (the
	// TUI's Send does while it is busy). Messages past it are dropped.
	logQueueSize = 32
)

// Server keeps one SMTC session in step with the player. Create it with
// NewServer, feed it every State from the player's Subscribe channel and Close
// it when done.
type Server struct {
	ctrl Controller
	logf func(string)
	now  func() time.Time

	mu      sync.Mutex
	bridge  bridge
	tracker tracker
	closed  bool

	events    chan event
	logs      chan string
	done      chan struct{}
	workers   sync.WaitGroup
	closeOnce sync.Once
}

// NewServer creates the SMTC session for ctrl. logf receives native errors and
// controller failures on a goroutine of its own, so it may block; it may also
// be nil. The session stays hidden from the system UI until Update sees a
// track.
func NewServer(ctrl Controller, logf func(string)) (*Server, error) {
	return newServer(ctrl, logf, newNativeBridge, time.Now)
}

func newServer(ctrl Controller, logf func(string), newBridge bridgeFactory, now func() time.Time) (*Server, error) {
	if logf == nil {
		logf = func(string) {}
	}
	s := &Server{
		ctrl:   ctrl,
		logf:   logf,
		now:    now,
		events: make(chan event, eventQueueSize),
		logs:   make(chan string, logQueueSize),
		done:   make(chan struct{}),
	}
	b, err := newBridge(s.onEvent, s.onError)
	if err != nil {
		return nil, err
	}
	s.bridge = b
	s.workers.Add(2)
	go s.run()
	go s.runLogs()
	return s, nil
}

// Update pushes the parts of st that SMTC readers have not seen yet. Calls
// that change nothing a reader shows cost no native call, so it is cheap to
// call on every 250 ms position tick. It never blocks on Windows.
func (s *Server) Update(st player.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if u, ok := s.tracker.next(st, s.now()); ok {
		s.bridge.apply(u)
	}
}

// Close removes the session from the system UI, stops event delivery, waits
// for a controller call already in progress and hands the remaining log
// messages to logf. It is safe to call more than once.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		// Not under mu: the native side waits for running event callbacks,
		// and those must never wait on mu.
		s.bridge.close()
		close(s.done)
		s.workers.Wait()
	})
	return nil
}

// onEvent queues a request for the worker. It runs on a Windows thread and
// returns at once.
func (s *Server) onEvent(e event) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case s.events <- e:
	default:
		s.log("[smtc] dropped " + e.kind.String() + ": controller is busy")
	}
}

// onError runs on the bridge thread and returns at once.
func (s *Server) onError(msg string) {
	s.log("[smtc] " + msg)
}

// log queues msg for logf without blocking.
func (s *Server) log(msg string) {
	select {
	case s.logs <- msg:
	default:
	}
}

func (s *Server) runLogs() {
	defer s.workers.Done()
	for {
		select {
		case msg := <-s.logs:
			s.logf(msg)
		case <-s.done:
			for {
				select {
				case msg := <-s.logs:
					s.logf(msg)
				default:
					return
				}
			}
		}
	}
}

// run calls the controller one request at a time, in arrival order, so a slow
// call (Next fetching a stream) cannot reorder or stall the ones behind it.
func (s *Server) run() {
	defer s.workers.Done()
	for {
		select {
		case <-s.done:
			return
		case e := <-s.events:
			if err := s.dispatch(e); err != nil {
				s.log(fmt.Sprintf("[smtc] %s: %v", e.kind, err))
			}
		}
	}
}

func (s *Server) dispatch(e event) error {
	switch e.kind {
	case eventPlay:
		return s.ctrl.Play()
	case eventPause, eventStop:
		// vibez has no stopped state that keeps the queue; MPRIS maps Stop to
		// Pause for the same reason.
		return s.ctrl.Pause()
	case eventNext:
		return s.ctrl.Next()
	case eventPrevious:
		return s.ctrl.Previous()
	case eventSeek:
		return s.ctrl.Seek(e.position)
	}
	return nil
}
