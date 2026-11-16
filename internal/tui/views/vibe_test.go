package views

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	_ = v.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	_ = v.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	_ = v.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})

	var got tea.Msg
	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	v.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if v.IsFocused() {
		t.Error("IsFocused() = true after Esc; want false")
	}
}

func TestVibe_Update_EnterWithEmptyInput_NoCmd(t *testing.T) {
	v := NewVibe()
	v.Focus()
	// Press Enter without typing anything — should return nil cmd.
	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("Update(Enter, empty input) returned non-nil cmd; want nil")
	}
}

func TestVibe_Update_NotFocused_ReturnsNil(t *testing.T) {
	v := NewVibe()
	// Not focused — all messages should return nil cmd.
	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	_ = v.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
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
	v.SetUsage("384 tok (290↑ 94↓)")
	v.SetResult(3, nil)
	lines := v.Lines(40, 8, 0)
	if len(lines) != 8 {
		t.Errorf("Lines(done) returned %d lines, want 8", len(lines))
	}
	narrowOut := strings.Join(lines, "\n")
	if !strings.Contains(narrowOut, "384 tok") || strings.Contains(narrowOut, "(290↑") {
		t.Errorf("narrow Lines(w=40) should render compact token usage, got:\n%s", narrowOut)
	}
	wideOut := strings.Join(v.Lines(60, 8, 0), "\n")
	if !strings.Contains(wideOut, "384 tok (290↑ 94↓)") {
		t.Errorf("wide Lines(w=60) should render full token usage, got:\n%s", wideOut)
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

func TestVibe_Update_PasteAndUTF8Sanitization(t *testing.T) {
	v := NewVibe()
	v.Focus()

	// Paste UTF-8 text with ANSI escapes, BiDi overrides, and trailing newline.
	_ = v.Update(tea.PasteMsg{Content: "\x1b[31mCaffè\x1b[0m\t\u202e久石譲\u202c\n"})

	var got tea.Msg
	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected VibeQueryMsg command after pasting valid text and pressing Enter")
	}
	got = cmd()
	qm, ok := got.(VibeQueryMsg)
	if !ok {
		t.Fatalf("got %T, want VibeQueryMsg", got)
	}
	if qm.Query != "Caffè 久石譲" {
		t.Errorf("VibeQueryMsg.Query = %q, want %q", qm.Query, "Caffè 久石譲")
	}
}

func TestVibe_Update_PasteOversizedIsBounded(t *testing.T) {
	v := NewVibe()
	v.Focus()

	_ = v.Update(tea.PasteMsg{Content: strings.Repeat("é", 500)})
	cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected VibeQueryMsg command")
	}
	qm := cmd().(VibeQueryMsg)
	if got := len([]rune(qm.Query)); got != vibeMaxRunes {
		t.Errorf("rune length = %d, want %d", got, vibeMaxRunes)
	}
}

func TestVibe_Update_PasteOnlyControlChars_Ignored(t *testing.T) {
	v := NewVibe()
	v.Focus()

	if cmd := v.Update(tea.PasteMsg{Content: "\x1b[2J\x00\x07\u202e\n\r\t"}); cmd != nil {
		t.Errorf("expected nil cmd when paste contains only unsafe/whitespace chars, got non-nil")
	}
	if cmd := v.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Errorf("expected nil cmd on Enter with empty sanitized input")
	}
}

func TestVibe_Lines_BackendEmojiBadge(t *testing.T) {
	cases := []struct {
		backend   string
		wantEmoji string
	}{
		{"omp:smol", "π"},
		{"ollama:qwen3:8b", "◉"},
		{"llamacpp:llama-3.2", "◉"},
		{"lmstudio:mistral", "◈"},
		{"apple-fm", "⌘"},
		{"claude:haiku", "✳"},
		{"gemini:flash", "✦"},
		{"codex:mini", "⎔"},
		{"copilot:gpt-4.1", "❖"},
		{"opencode:default", "⟡"},
		{"llm:default", "◇"},
		{"keyword", "⌕"},
	}
	for _, tc := range cases {
		if got := BackendEmoji(tc.backend); got != tc.wantEmoji {
			t.Errorf("BackendEmoji(%q) = %q, want %q", tc.backend, got, tc.wantEmoji)
		}
		if w := ansi.StringWidth(tc.wantEmoji); w != 1 {
			t.Errorf("glyph %q for %q has cell width %d, want 1", tc.wantEmoji, tc.backend, w)
		}
		if tc.backend == "keyword" {
			continue
		}
		v := NewVibe()
		v.SetBackend(tc.backend)
		badge := v.BackendBadge()
		if !strings.Contains(badge, tc.wantEmoji) || !strings.Contains(badge, tc.backend) {
			t.Errorf("BackendBadge() for %q = %q, want %q and backend name", tc.backend, badge, tc.wantEmoji)
		}
		out := strings.Join(v.Lines(60, 10, 0), "\n")
		if strings.Contains(out, tc.backend) {
			t.Errorf("Lines() should not duplicate backend badge %q inside card body:\n%s", tc.backend, out)
		}
	}
}

func TestVibe_Lines_BearFixedWidthNoCombiningMarks(t *testing.T) {
	checkState := func(name string, setup func(*VibeModel), anchor string) {
		t.Helper()
		v := NewVibe()
		setup(v)
		wantWidth := -1
		for step := range 50 {
			lines := v.Lines(48, 10, step)
			found := false
			for _, l := range lines {
				plain := ansi.Strip(l)
				for _, r := range plain {
					if unicode.Is(unicode.Mn, r) {
						t.Fatalf("%s step=%d line %q contains combining mark %U", name, step, plain, r)
					}
				}
				if strings.Contains(plain, anchor) {
					found = true
					w := ansi.StringWidth(plain)
					if wantWidth < 0 {
						wantWidth = w
					} else if w != wantWidth {
						t.Fatalf("%s step=%d bear status line width = %d, want constant %d (%q)", name, step, w, wantWidth, plain)
					}
				}
			}
			if !found {
				t.Fatalf("%s step=%d missing anchor %q in %v", name, step, anchor, lines)
			}
		}
	}

	checkState("idle", func(*VibeModel) {}, "press v to start")
	checkState("searching", func(v *VibeModel) {
		v.Focus()
		v.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	}, "searching…")
	checkState("done", func(v *VibeModel) {
		v.SetSearching()
		v.SetResult(2, nil)
	}, "✓ added 2 tracks")
	checkState("error", func(v *VibeModel) {
		v.SetSearching()
		v.SetResult(0, errors.New("none"))
	}, "no results")
}

func TestVibe_Lines_NarrowWidthsNeverPanic(t *testing.T) {
	widths := []int{0, 1, 2, 4, 8, 16}
	heights := []int{1, 3, 8}
	for _, w := range widths {
		for _, h := range heights {
			v := NewVibe()
			v.SetSearching()
			v.lastQ = "extremely long vibe query string"
			_ = v.Lines(w, h, 0)

			v.SetResult(3, nil)
			v.explanation = "warm ambient electronic mix"
			v.SetUsage("384 tok (290↑ 94↓)")
			_ = v.Lines(w, h, 1)

			v.SetResult(0, errors.New("none"))
			_ = v.Lines(w, h, 2)

			v.SetDiscovery(DiscoveryInfo{
				Active:     true,
				SeedArtist: "Boards of Canada",
				SeedTitle:  "Roygbiv",
				Similarity: -0.5,
			})
			_ = v.Lines(w, h, 3)

			v.SetDiscovery(DiscoveryInfo{
				Active:     true,
				SeedArtist: "Boards of Canada",
				SeedTitle:  "Roygbiv",
				Similarity: 2.0,
			})
			_ = v.Lines(w, h, 4)
		}
	}
}
