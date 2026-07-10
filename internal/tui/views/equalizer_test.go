package views

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/player"
)

func TestEQModel_Update(t *testing.T) {
	bands := []player.EQBand{
		{Frequency: 32, Gain: 2.0},
		{Frequency: 64, Gain: -1.0},
		{Frequency: 125, Gain: 0.0},
	}
	m := NewEqualizer(bands)

	// Verify initial cursor is 0
	if m.cursor != 0 {
		t.Fatalf("expected initial cursor to be 0, got %d", m.cursor)
	}

	// Move right
	m.Update(tea.KeyPressMsg{Text: "right"})
	if m.cursor != 1 {
		t.Errorf("expected cursor to move to 1, got %d", m.cursor)
	}

	// Move right again
	m.Update(tea.KeyPressMsg{Text: "l"})
	if m.cursor != 2 {
		t.Errorf("expected cursor to move to 2, got %d", m.cursor)
	}

	// Move right beyond limits
	m.Update(tea.KeyPressMsg{Text: "right"})
	if m.cursor != 2 {
		t.Errorf("expected cursor to clamp at 2, got %d", m.cursor)
	}

	// Move left
	m.Update(tea.KeyPressMsg{Text: "left"})
	if m.cursor != 1 {
		t.Errorf("expected cursor to move back to 1, got %d", m.cursor)
	}

	// Move left again
	m.Update(tea.KeyPressMsg{Text: "h"})
	if m.cursor != 0 {
		t.Errorf("expected cursor to move back to 0, got %d", m.cursor)
	}

	// Move left beyond limits
	m.Update(tea.KeyPressMsg{Text: "left"})
	if m.cursor != 0 {
		t.Errorf("expected cursor to clamp at 0, got %d", m.cursor)
	}

	// Gain up on cursor 0 (initial gain: 2.0)
	cmd := m.Update(tea.KeyPressMsg{Text: "up"})
	if cmd == nil {
		t.Fatal("expected EQChangeMsg command, got nil")
	}
	msg := cmd()
	eqMsg, ok := msg.(EQChangeMsg)
	if !ok {
		t.Fatalf("expected EQChangeMsg, got %T", msg)
	}
	if eqMsg.Bands[0].Gain != 2.5 {
		t.Errorf("expected gain to increase to 2.5, got %f", eqMsg.Bands[0].Gain)
	}

	// Gain up using 'k'
	_ = m.Update(tea.KeyPressMsg{Text: "k"})
	if m.bands[0].Gain != 3.0 {
		t.Errorf("expected gain to increase to 3.0, got %f", m.bands[0].Gain)
	}

	// Gain down on cursor 0 (current gain: 3.0)
	_ = m.Update(tea.KeyPressMsg{Text: "down"})
	if m.bands[0].Gain != 2.5 {
		t.Errorf("expected gain to decrease to 2.5, got %f", m.bands[0].Gain)
	}

	// Gain down using 'j'
	_ = m.Update(tea.KeyPressMsg{Text: "j"})
	if m.bands[0].Gain != 2.0 {
		t.Errorf("expected gain to decrease to 2.0, got %f", m.bands[0].Gain)
	}

	// Reset active band with '0'
	_ = m.Update(tea.KeyPressMsg{Text: "0"})
	if m.bands[0].Gain != 0.0 {
		t.Errorf("expected gain to reset to 0.0, got %f", m.bands[0].Gain)
	}

	// Reset all bands with 'r'
	m.bands[0].Gain = 5.0
	m.bands[1].Gain = -5.0
	_ = m.Update(tea.KeyPressMsg{Text: "r"})
	for i, b := range m.bands {
		if b.Gain != 0.0 {
			t.Errorf("expected band %d gain to be reset, got %f", i, b.Gain)
		}
	}
}
