package tui

import (
	"context"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestLoginCommand_DoesNotDeadlockEventLoop(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 100, 30
	m.mode = modeCommand
	m.cmdBuf = "login"

	var prog *tea.Program
	type probeMsg struct{}
	probeReceived := make(chan struct{})
	m.onReauth = func() tea.Cmd {
		return func() tea.Msg {
			// A send from Update would block the very loop that receives it.
			prog.Send(SessionExpiredMsg{})
			return probeMsg{}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prog = tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutSignals(),
		tea.WithoutRenderer(),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if _, ok := msg.(probeMsg); ok {
				close(probeReceived)
				return tea.Quit()
			}
			return msg
		}),
	)
	go prog.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

	if _, err := prog.Run(); err != nil {
		t.Fatalf("Bubble Tea exited before processing the login command: %v", err)
	}
	select {
	case <-probeReceived:
	default:
		t.Fatal("event loop did not receive a message after :login's Program.Send")
	}
}
