package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// --- mock player ---

type mockPlayer struct {
	state       player.State
	playCalled  bool
	pauseCalled bool
	nextCalled  bool
	prevCalled  bool
	closeCalled bool
	err         error
	stateCh     chan player.State
}

func newMockPlayer() *mockPlayer {
	return &mockPlayer{stateCh: make(chan player.State, 4)}
}

func (m *mockPlayer) Play() error                { m.playCalled = true; return m.err }
func (m *mockPlayer) Pause() error               { m.pauseCalled = true; return m.err }
func (m *mockPlayer) Stop() error                { return m.err }
func (m *mockPlayer) Next() error                { m.nextCalled = true; return m.err }
func (m *mockPlayer) Previous() error            { m.prevCalled = true; return m.err }
func (m *mockPlayer) Seek(_ time.Duration) error { return m.err }
func (m *mockPlayer) SetVolume(_ float64) error  { return m.err }
func (m *mockPlayer) SetQueue(_ []string) error  { return m.err }
func (m *mockPlayer) GetState() (*player.State, error) {
	s := m.state
	return &s, m.err
}
func (m *mockPlayer) Subscribe() <-chan player.State { return m.stateCh }
func (m *mockPlayer) Close() error                   { m.closeCalled = true; return m.err }

// --- mock provider ---

type mockProvider struct{}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Search(_ context.Context, _ string) (*provider.SearchResult, error) {
	return &provider.SearchResult{}, nil
}
func (m *mockProvider) GetLibraryTracks(_ context.Context) ([]provider.Track, error) {
	return nil, nil
}
func (m *mockProvider) GetLibraryPlaylists(_ context.Context) ([]provider.Playlist, error) {
	return nil, nil
}
func (m *mockProvider) GetPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (m *mockProvider) GetAlbumTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (m *mockProvider) IsAuthenticated() bool { return true }

// --- helpers ---

func testCfg() *config.Config {
	return &config.Config{
		StoreFront: "us",
		AuthPort:   7777,
		Provider:   "apple",
		Theme:      "default",
	}
}

func newModel(plyr player.Player) *Model {
	return New(testCfg(), &mockProvider{}, plyr)
}

// --- clamp ---

func TestClamp_Middle(t *testing.T) {
	got := clamp(0.5, 0, 1)
	if got != 0.5 {
		t.Errorf("clamp(0.5,0,1) = %v, want 0.5", got)
	}
}

func TestClamp_BelowLo(t *testing.T) {
	got := clamp(-1, 0, 1)
	if got != 0 {
		t.Errorf("clamp(-1,0,1) = %v, want 0", got)
	}
}

func TestClamp_AboveHi(t *testing.T) {
	got := clamp(2, 0, 1)
	if got != 1 {
		t.Errorf("clamp(2,0,1) = %v, want 1", got)
	}
}

func TestClamp_AtLoBoundary(t *testing.T) {
	got := clamp(0, 0, 1)
	if got != 0 {
		t.Errorf("clamp(0,0,1) = %v, want 0", got)
	}
}

func TestClamp_AtHiBoundary(t *testing.T) {
	got := clamp(1, 0, 1)
	if got != 1 {
		t.Errorf("clamp(1,0,1) = %v, want 1", got)
	}
}

// --- max ---

func TestMax_SecondLarger(t *testing.T) {
	if max(3, 5) != 5 {
		t.Errorf("max(3,5) != 5")
	}
}

func TestMax_FirstLarger(t *testing.T) {
	if max(5, 3) != 5 {
		t.Errorf("max(5,3) != 5")
	}
}

func TestMax_Equal(t *testing.T) {
	if max(3, 3) != 3 {
		t.Errorf("max(3,3) != 3")
	}
}

// --- Model construction ---

func TestNew_NilPlayer(t *testing.T) {
	m := newModel(nil)
	if m == nil {
		t.Fatal("New(cfg, prov, nil) returned nil")
	}
}

func TestNew_WithPlayer(t *testing.T) {
	m := newModel(newMockPlayer())
	if m == nil {
		t.Fatal("New with player returned nil")
	}
	if m.stateCh == nil {
		t.Error("stateCh should be set when player is provided")
	}
}

func TestModel_Init(t *testing.T) {
	m := newModel(nil)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return non-nil cmd")
	}
}

// --- View ---

func TestModel_View_WidthZero(t *testing.T) {
	m := newModel(nil)
	got := m.View()
	if got != "Loading…" {
		t.Errorf("View() with width=0 should return 'Loading…', got %q", got)
	}
}

func TestModel_View_WithDimensions(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.height = 24
	m.nowPlaying.SetSize(80, 22)
	got := m.View()
	if got == "" {
		t.Error("View() with dimensions should return non-empty string")
	}
}

// --- Update: WindowSizeMsg ---

func TestModel_Update_WindowSizeMsg(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.width != 100 || m.height != 30 {
		t.Errorf("width=%d height=%d, want 100 30", m.width, m.height)
	}
}

// --- Update: tickMsg ---

func TestModel_Update_TickMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Error("tickMsg should return a non-nil cmd (reschedule tick)")
	}
}

func TestModel_Update_TickMsg_ClearsExpiredErr(t *testing.T) {
	m := newModel(nil)
	m.errMsg = "old error"
	m.errExpiry = time.Now().Add(-1 * time.Second) // already expired
	m.Update(tickMsg(time.Now()))
	if m.errMsg != "" {
		t.Errorf("expired errMsg should be cleared, got %q", m.errMsg)
	}
}

// --- Update: errMsg ---

func TestModel_Update_ErrMsg(t *testing.T) {
	m := newModel(nil)
	m.Update(errMsg{err: errors.New("test error")})
	if m.errMsg != "test error" {
		t.Errorf("errMsg = %q, want %q", m.errMsg, "test error")
	}
}

// --- Update: key messages ---

func TestModel_Update_KeyViewNow(t *testing.T) {
	m := newModel(nil)
	m.activeView = viewQueue
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.activeView != viewNowPlaying {
		t.Errorf("activeView = %d, want viewNowPlaying(%d)", m.activeView, viewNowPlaying)
	}
}

func TestModel_Update_KeyViewQueue(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.activeView != viewQueue {
		t.Errorf("activeView = %d, want viewQueue(%d)", m.activeView, viewQueue)
	}
}

func TestModel_Update_KeyViewLibrary(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.activeView != viewLibrary {
		t.Errorf("activeView = %d, want viewLibrary(%d)", m.activeView, viewLibrary)
	}
}

func TestModel_Update_KeyViewSearch(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.activeView != viewSearch {
		t.Errorf("activeView = %d, want viewSearch(%d)", m.activeView, viewSearch)
	}
}

func TestModel_Update_KeyHelp(t *testing.T) {
	m := newModel(nil)
	m.showHelp = false
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !m.showHelp {
		t.Error("showHelp should be true after '?' key")
	}
}

func TestModel_Update_KeyQuit_NilPlayer(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("q key should return non-nil cmd (Quit)")
	}
	// Execute cmd and verify it returns a quit message.
	msg := cmd()
	if msg != tea.Quit() {
		t.Errorf("q key cmd should return tea.Quit(), got %v", msg)
	}
}

func TestModel_Update_KeyQuit_WithPlayer(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("q key should return non-nil cmd")
	}
	if !mp.closeCalled {
		t.Error("player.Close() should be called on quit")
	}
}

// --- togglePlayPause ---

func TestTogglePlayPause_NilPlayer(t *testing.T) {
	m := newModel(nil)
	cmd := m.togglePlayPause()
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("togglePlayPause with nil player should return errMsg, got %T", msg)
	}
}

func TestTogglePlayPause_Playing_CallsPause(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Playing = true
	cmd := m.togglePlayPause()
	cmd() // execute
	if !mp.pauseCalled {
		t.Error("Pause() should be called when state is playing")
	}
}

func TestTogglePlayPause_NotPlaying_CallsPlay(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Playing = false
	cmd := m.togglePlayPause()
	cmd() // execute
	if !mp.playCalled {
		t.Error("Play() should be called when state is not playing")
	}
}

func TestTogglePlayPause_PlayerError(t *testing.T) {
	mp := newMockPlayer()
	mp.err = errors.New("player error")
	m := newModel(mp)
	m.playerState.Playing = true
	cmd := m.togglePlayPause()
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("togglePlayPause with player error should return errMsg, got %T", msg)
	}
}

// --- playerCmd ---

func TestPlayerCmd_NilPlayer(t *testing.T) {
	m := newModel(nil)
	cmd := m.playerCmd(func() error { return nil })
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("playerCmd with nil player should return errMsg, got %T", msg)
	}
}

func TestPlayerCmd_Next(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	cmd := m.playerCmd(func() error { return mp.Next() })
	msg := cmd()
	if msg != nil {
		t.Errorf("playerCmd success should return nil msg, got %v", msg)
	}
	if !mp.nextCalled {
		t.Error("Next() should have been called")
	}
}

func TestPlayerCmd_Previous(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	cmd := m.playerCmd(func() error { return mp.Previous() })
	cmd()
	if !mp.prevCalled {
		t.Error("Previous() should have been called")
	}
}

// --- adjustVolume ---

func TestAdjustVolume_NilPlayer(t *testing.T) {
	m := newModel(nil)
	cmd := m.adjustVolume(0.1)
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("adjustVolume with nil player should return errMsg, got %T", msg)
	}
}

func TestAdjustVolume_ClampHi(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Volume = 0.99
	cmd := m.adjustVolume(0.05) // would exceed 1.0
	msg := cmd()
	if msg != nil {
		t.Errorf("adjustVolume should succeed, got %v", msg)
	}
}

func TestAdjustVolume_ClampLo(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Volume = 0.01
	cmd := m.adjustVolume(-0.05) // would go below 0
	msg := cmd()
	if msg != nil {
		t.Errorf("adjustVolume should succeed, got %v", msg)
	}
}

// --- Key: space (play/pause) ---

func TestModel_KeySpace_NilPlayer(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("space key should return non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(errMsg); !ok {
		t.Errorf("space with nil player should produce errMsg, got %T", msg)
	}
}

func TestModel_KeyNext_WithPlayer(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd != nil {
		cmd() // execute to trigger Next()
	}
	if !mp.nextCalled {
		t.Error("n key should call player.Next()")
	}
}

func TestModel_KeyPrevious_WithPlayer(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if cmd != nil {
		cmd() // execute to trigger Previous()
	}
	if !mp.prevCalled {
		t.Error("p key should call player.Previous()")
	}
}

func TestModel_KeyVolumeUp(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Volume = 0.5
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	if cmd != nil {
		cmd()
	}
}

func TestModel_KeyVolumeDown(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Volume = 0.5
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")})
	if cmd != nil {
		cmd()
	}
}

// --- playerStateMsg ---

func TestModel_Update_PlayerStateMsg(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := &provider.Track{Title: "Live Track", Artist: "Live Artist"}
	msg := playerStateMsg{
		Track:   track,
		Playing: true,
		Volume:  0.8,
	}
	m.Update(msg)
	if m.playerState.Track == nil {
		t.Error("playerState.Track should be set after playerStateMsg")
	}
	if m.playerState.Track.Title != "Live Track" {
		t.Errorf("Track.Title = %q, want %q", m.playerState.Track.Title, "Live Track")
	}
}

// --- contentHeight ---

func TestModel_ContentHeight(t *testing.T) {
	m := newModel(nil)
	m.height = 26
	got := m.contentHeight()
	// header + sep + sep + footer = 4 lines
	if got != 22 {
		t.Errorf("contentHeight() = %d, want 22", got)
	}
}

func TestModel_ContentHeight_Small(t *testing.T) {
	m := newModel(nil)
	m.height = 1
	got := m.contentHeight()
	if got < 0 {
		t.Errorf("contentHeight() should not be negative, got %d", got)
	}
}

// --- renderHeader ---

func TestModel_RenderHeader_ContainsVibez(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	got := m.renderHeader()
	if !strings.Contains(got, "vibez") {
		t.Errorf("renderHeader() should contain 'vibez', got %q", got)
	}
}

// --- renderFooter ---

func TestModel_RenderFooter_ContainsKeyHints(t *testing.T) {
	m := newModel(nil)
	m.width = 100
	got := m.renderFooter()
	if !strings.Contains(got, "space") {
		t.Errorf("renderFooter() should contain key hints, got %q", got)
	}
}

// --- View with error message ---

func TestModel_View_WithErrMsg(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.height = 24
	m.nowPlaying.SetSize(80, 22)
	m.errMsg = "something went wrong"
	m.errExpiry = time.Now().Add(10 * time.Second)
	got := m.View()
	if !strings.Contains(got, "something went wrong") {
		t.Errorf("View() should contain error message, got %q", got)
	}
}

// --- updateActiveView ---

func TestModel_UpdateActiveView_Library(t *testing.T) {
	m := newModel(nil)
	m.activeView = viewLibrary
	m.width = 80
	m.height = 24
	m.library.SetSize(80, 22)
	// Should not panic
	m.updateActiveView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
}

func TestModel_UpdateActiveView_Search(t *testing.T) {
	m := newModel(nil)
	m.activeView = viewSearch
	m.width = 80
	m.height = 24
	m.search.SetSize(80, 22)
	// Should not panic
	m.updateActiveView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
}

func TestModel_UpdateActiveView_Queue(t *testing.T) {
	m := newModel(nil)
	m.activeView = viewQueue
	// Should not panic
	m.updateActiveView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
}

func TestModel_UpdateActiveView_NowPlaying(t *testing.T) {
	m := newModel(nil)
	m.activeView = viewNowPlaying
	// Should not panic
	m.updateActiveView(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
}

// --- Search focused: keys go to search input ---

func TestModel_SearchFocused_KeyGoesToSearch(t *testing.T) {
	m := newModel(nil)
	m.activeView = viewSearch
	m.search.Focus()
	// When search is focused, key messages should go to the search view
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	_ = cmd // just verify no panic
}

// --- waitForState inner function ---

func TestWaitForState_ReadsFromChannel(t *testing.T) {
	ch := make(chan player.State, 1)
	st := player.State{Playing: true, Volume: 0.9}
	ch <- st

	cmd := waitForState(ch)
	msg := cmd() // should not block since channel has a value

	ps, ok := msg.(playerStateMsg)
	if !ok {
		t.Fatalf("waitForState returned %T, want playerStateMsg", msg)
	}
	if !ps.Playing {
		t.Error("playerStateMsg.Playing should be true")
	}
	if ps.Volume != 0.9 {
		t.Errorf("playerStateMsg.Volume = %v, want 0.9", ps.Volume)
	}
}
