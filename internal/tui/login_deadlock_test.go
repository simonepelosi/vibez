package tui

import (
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestLoginCommand_DoesNotDeadlockEventLoop runs the real Bubble Tea event
// loop with OnReauth wired the way cmd/browserless_flow.go wires it: the
// callback calls prog.Send(SessionExpiredMsg{}). Program.Send writes to an
// unbuffered channel that only the event loop reads, so if :login invokes
// OnReauth synchronously from Update the send can never complete.
func TestLoginCommand_DoesNotDeadlockEventLoop(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 100, 30
	m.mode = modeCommand
	m.cmdBuf = "login"

	var prog *tea.Program
	reauthReturned := make(chan struct{})
	m.onReauth = func() {
		prog.Send(SessionExpiredMsg{}) // mirrors startReauth in cmd/browserless_flow.go
		close(reauthReturned)
	}

	prog = tea.NewProgram(m,
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutSignals(),
		tea.WithoutRenderer(),
	)
	runDone := make(chan struct{})
	go func() {
		_, _ = prog.Run()
		close(runDone)
	}()
	t.Cleanup(func() {
		prog.Kill()
		<-runDone
	})

	prog.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case <-reauthReturned:
	case <-time.After(3 * time.Second):
		t.Fatal(":login deadlocked the TUI: OnReauth's prog.Send never returned because it was called from inside Update")
	}

	// The event loop must still be responsive afterwards.
	probe := make(chan struct{})
	go func() {
		prog.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
		close(probe)
	}()
	select {
	case <-probe:
	case <-time.After(3 * time.Second):
		t.Fatal("event loop unresponsive after :login")
	}
}
