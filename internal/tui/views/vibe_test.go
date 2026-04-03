package views

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// --- NewVibe ---

func TestNewVibe_NotNil(t *testing.T) {
	v := NewVibe()
	if v == nil {
		t.Fatal("NewVibe() returned nil")
	}
}

func TestNewVibe_NotFocused(t *testing.T) {
	v := NewVibe()
	if v.IsFocused() {
		t.Error("NewVibe should not be focused initially")
	}
}

func TestNewVibe_DiscoveryNotActive(t *testing.T) {
	v := NewVibe()
	if v.DiscoveryActive() {
		t.Error("NewVibe should not have discovery active initially")
	}
}

// --- Focus / IsFocused ---

func TestVibe_Focus_ActivatesInput(t *testing.T) {
	v := NewVibe()
	v.Focus()
	if !v.IsFocused() {
		t.Error("IsFocused() = false after Focus()")
	}
}

func TestVibe_Focus_NoopDuringSearch(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching() // transitions to vibeSearching
	v.Focus()        // should be a no-op
	if v.IsFocused() {
		t.Error("Focus() should be a no-op during vibeSearching; IsFocused() = true")
	}
}

// --- SetSearching ---

func TestVibe_SetSearching_BlursInput(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	if v.IsFocused() {
		t.Error("IsFocused() = true after SetSearching; want false")
	}
}

// --- SetResult ---

func TestVibe_SetResult_Success(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	v.SetResult(5, nil)
	if v.IsFocused() {
		t.Error("should not be focused after SetResult(success)")
	}
}

func TestVibe_SetResult_Error(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	v.SetResult(0, errors.New("no results"))
	if v.IsFocused() {
		t.Error("should not be focused after SetResult(error)")
	}
}

func TestVibe_SetResult_ZeroTracksNoError(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	v.SetResult(0, nil) // success with 0 tracks — transitions to vibeDone
	// Should not panic.
}

// --- SetDiscovery ---

func TestVibe_SetDiscovery_Active(t *testing.T) {
	v := NewVibe()
	v.SetDiscovery(DiscoveryInfo{Active: true, SeedArtist: "Frank Ocean", SeedTitle: "Nights"})
	if !v.DiscoveryActive() {
		t.Error("DiscoveryActive() = false after SetDiscovery(active=true)")
	}
}

func TestVibe_SetDiscovery_Inactive(t *testing.T) {
	v := NewVibe()
	v.SetDiscovery(DiscoveryInfo{Active: true})
	v.SetDiscovery(DiscoveryInfo{Active: false})
	if v.DiscoveryActive() {
		t.Error("DiscoveryActive() = true after SetDiscovery(active=false)")
	}
}

func TestVibe_SetDiscovery_BlursInput(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetDiscovery(DiscoveryInfo{Active: true})
	if v.IsFocused() {
		t.Error("IsFocused() = true after SetDiscovery(active=true); want false")
	}
}

// --- Update ---

func TestVibe_Update_EnterSubmitsQuery(t *testing.T) {
	v := NewVibe()
	v.Focus()

	// Type some text first (simulated via direct model manipulation using Update).
	_ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	_ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	_ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	var got tea.Msg
	cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		got = cmd()
	}

	if got != nil {
		if _, ok := got.(VibeQueryMsg); !ok {
			t.Errorf("Enter cmd returned %T, want VibeQueryMsg", got)
		}
	}
	// After enter the model transitions to searching and is no longer focused.
	if v.IsFocused() {
		t.Error("IsFocused() = true after Enter; want false")
	}
}

func TestVibe_Update_EscCancels(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if v.IsFocused() {
		t.Error("IsFocused() = true after Esc; want false")
	}
}

func TestVibe_Update_EnterWithEmptyInput_NoCmd(t *testing.T) {
	v := NewVibe()
	v.Focus()
	// Press Enter without typing anything — should return nil cmd.
	cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("Update(Enter, empty input) returned non-nil cmd; want nil")
	}
}

func TestVibe_Update_NotFocused_ReturnsNil(t *testing.T) {
	v := NewVibe()
	// Not focused — all messages should return nil cmd.
	cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("Update when not focused should return nil cmd")
	}
}

func TestVibe_Update_NonKeyMsg_ReturnsNil(t *testing.T) {
	v := NewVibe()
	v.Focus()
	cmd := v.Update("not-a-key-message")
	if cmd != nil {
		t.Error("Update(non-KeyMsg) should return nil cmd")
	}
}

// --- Lines ---

func TestVibe_Lines_IdleState(t *testing.T) {
	v := NewVibe()
	lines := v.Lines(40, 8, 0)
	if len(lines) != 8 {
		t.Errorf("Lines returned %d lines, want 8", len(lines))
	}
}

func TestVibe_Lines_InputtingState(t *testing.T) {
	v := NewVibe()
	v.Focus()
	lines := v.Lines(40, 8, 0)
	if len(lines) != 8 {
		t.Errorf("Lines(inputting) returned %d lines, want 8", len(lines))
	}
}

func TestVibe_Lines_SearchingState(t *testing.T) {
	v := NewVibe()
	v.Focus()
	_ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	v.SetSearching()
	lines := v.Lines(40, 8, 0)
	if len(lines) != 8 {
		t.Errorf("Lines(searching) returned %d lines, want 8", len(lines))
	}
}

func TestVibe_Lines_DoneState(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	v.SetResult(3, nil)
	lines := v.Lines(40, 8, 0)
	if len(lines) != 8 {
		t.Errorf("Lines(done) returned %d lines, want 8", len(lines))
	}
}

func TestVibe_Lines_ErrorState(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	v.SetResult(0, errors.New("no results found"))
	lines := v.Lines(40, 10, 0)
	if len(lines) != 10 {
		t.Errorf("Lines(error) returned %d lines, want 10", len(lines))
	}
	// Error text should appear somewhere in the lines.
	found := false
	for _, l := range lines {
		if strings.Contains(l, "no results") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Lines(error) should contain 'no results', got %v", lines)
	}
}

func TestVibe_Lines_DiscoveryState(t *testing.T) {
	v := NewVibe()
	v.SetDiscovery(DiscoveryInfo{
		Active:     true,
		SeedArtist: "Frank Ocean",
		SeedTitle:  "Nights",
		Similarity: 0.75,
	})
	lines := v.Lines(60, 12, 0)
	if len(lines) != 12 {
		t.Errorf("Lines(discovery) returned %d lines, want 12", len(lines))
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Frank Ocean") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Lines(discovery) should contain SeedArtist, got %v", lines)
	}
}

func TestVibe_Lines_DiscoveryRefilling(t *testing.T) {
	v := NewVibe()
	v.SetDiscovery(DiscoveryInfo{
		Active:     true,
		SeedArtist: "SZA",
		SeedTitle:  "Good Days",
		Similarity: 0.5,
		Refilling:  true,
	})
	lines := v.Lines(60, 12, 0)
	if len(lines) != 12 {
		t.Errorf("Lines(discovery refilling) returned %d lines, want 12", len(lines))
	}
}

func TestVibe_Lines_PadsToHeight(t *testing.T) {
	v := NewVibe()
	// Provide a large height — Lines must always return exactly h lines.
	lines := v.Lines(40, 20, 0)
	if len(lines) != 20 {
		t.Errorf("Lines(h=20) returned %d lines, want 20", len(lines))
	}
}

func TestVibe_Lines_AnimationSteps(t *testing.T) {
	v := NewVibe()
	for step := range 50 {
		lines := v.Lines(40, 8, step)
		if len(lines) != 8 {
			t.Errorf("Lines(step=%d) returned %d lines, want 8", step, len(lines))
		}
	}
}

func TestVibe_SetResult_SingleTrackSingular(t *testing.T) {
	v := NewVibe()
	v.Focus()
	v.SetSearching()
	v.SetResult(1, nil)
	lines := v.Lines(40, 8, 0)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "1 track") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Lines should contain '1 track' (singular) for added=1, got %v", lines)
	}
}
