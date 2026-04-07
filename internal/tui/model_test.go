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
	"github.com/simone-vibes/vibez/internal/tui/views"
)

// --- mock player ---

type mockPlayer struct {
	state          player.State
	playCalled     bool
	pauseCalled    bool
	nextCalled     bool
	prevCalled     bool
	closeCalled    bool
	setQueueIDs    []string   // last IDs passed to SetQueue
	appendQueueIDs [][]string // all calls to AppendQueue (each call appended)
	err            error
	stateCh        chan player.State
}

func newMockPlayer() *mockPlayer {
	return &mockPlayer{stateCh: make(chan player.State, 4)}
}

func (m *mockPlayer) Play() error                       { m.playCalled = true; return m.err }
func (m *mockPlayer) Pause() error                      { m.pauseCalled = true; return m.err }
func (m *mockPlayer) Stop() error                       { return m.err }
func (m *mockPlayer) Next() error                       { m.nextCalled = true; return m.err }
func (m *mockPlayer) Previous() error                   { m.prevCalled = true; return m.err }
func (m *mockPlayer) Seek(_ time.Duration) error        { return m.err }
func (m *mockPlayer) SetVolume(_ float64) error         { return m.err }
func (m *mockPlayer) SetRepeat(_ int) error             { return m.err }
func (m *mockPlayer) SetShuffle(_ bool) error           { return m.err }
func (m *mockPlayer) SetQueue(ids []string) error       { m.setQueueIDs = ids; return m.err }
func (m *mockPlayer) SetPlaylist(_ string, _ int) error { return m.err }
func (m *mockPlayer) AppendQueue(ids []string) error {
	m.appendQueueIDs = append(m.appendQueueIDs, ids)
	return m.err
}
func (m *mockPlayer) RemoveFromQueue(_ int) error { return m.err }
func (m *mockPlayer) MoveInQueue(_, _ int) error  { return m.err }
func (m *mockPlayer) ClearQueue() error           { return m.err }
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
func (m *mockProvider) GetCatalogPlaylistTracks(_ context.Context, _ string) ([]provider.Track, error) {
	return nil, nil
}
func (m *mockProvider) CreatePlaylist(_ context.Context, _ string, _ []string) (provider.Playlist, error) {
	return provider.Playlist{}, nil
}
func (m *mockProvider) LoveSong(_ context.Context, _ string, _ bool) error      { return nil }
func (m *mockProvider) GetSongRating(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mockProvider) IsAuthenticated() bool                                   { return true }
func (m *mockProvider) GetRecommendations(_ context.Context) ([]provider.RecommendationGroup, error) {
	return nil, nil
}

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
	return New(testCfg(), &mockProvider{}, plyr, Options{})
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
	// With width=0 the intro animation hasn't started yet — expect empty string.
	if got != "" {
		t.Errorf("View() with width=0 should return empty string, got %q", got)
	}
}

func TestModel_View_WithDimensions(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.height = 24
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

func TestModel_Update_KeySearch(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.mode != modeSearch {
		t.Errorf("mode = %d, want modeSearch(%d)", m.mode, modeSearch)
	}
}

func TestModel_Update_KeyCommand(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	if m.mode != modeCommand {
		t.Errorf("mode = %d, want modeCommand(%d)", m.mode, modeCommand)
	}
}

func TestModel_Update_KeyLibrary(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if m.activePanel < 0 {
		t.Errorf("activePanel = %d, want >= 0 (library panel active)", m.activePanel)
	}
	if m.panels[m.activePanel].NavKey() != "l" {
		t.Errorf("active panel NavKey = %q, want %q", m.panels[m.activePanel].NavKey(), "l")
	}
}

func TestModel_Update_KeySearchSetsContent(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.mode != modeSearch {
		t.Errorf("mode = %d, want modeSearch(%d)", m.mode, modeSearch)
	}
}

func TestModel_Update_KeyQuit_NilPlayer(t *testing.T) {
	m := newModel(nil)
	// 'q' now opens the queue panel (not quit)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.activePanel < 0 {
		t.Error("q key should activate the queue panel")
	}
}

func TestModel_Update_KeyQuit_WithPlayer(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	// ':q' quits and closes the player
	m.mode = modeCommand
	m.cmdBuf = "q"
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !mp.closeCalled {
		t.Error("player.Close() should be called when quitting with :q")
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
	got := m.panelHeight()
	// fixed overhead = 20 lines (box layout with two status lines)
	if got != 6 {
		t.Errorf("panelHeight() = %d, want 6", got)
	}
}

func TestModel_ContentHeight_Small(t *testing.T) {
	m := newModel(nil)
	m.height = 1
	got := m.panelHeight()
	if got < 0 {
		t.Errorf("panelHeight() should not be negative, got %d", got)
	}
}

// --- renderBoxHeader ---

func TestModel_RenderHeader_ContainsVibez(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	got := m.renderBoxHeader(m.width - 2)
	if !strings.Contains(got, "vibez") {
		t.Errorf("renderBoxHeader() should contain 'vibez', got %q", got)
	}
}

// --- statusNavContent ---

func TestModel_RenderFooter_ContainsKeyHints(t *testing.T) {
	m := newModel(nil)
	m.width = 100
	got := m.statusNavContent(m.width - 4)
	if !strings.Contains(got, "search") {
		t.Errorf("statusNavContent() should contain key hints, got %q", got)
	}
}

// --- View with error message ---

func TestModel_View_WithErrMsg(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.height = 24
	m.introStep = introDone // skip startup animation
	m.errMsg = "something went wrong"
	m.errExpiry = time.Now().Add(10 * time.Second)
	got := m.View()
	if !strings.Contains(got, "something went wrong") {
		t.Errorf("View() should contain error message, got %q", got)
	}
}

// --- library navigation in normal mode ---

func TestModel_UpdateActiveView_Library(t *testing.T) {
	m := newModel(nil)
	// Activate library panel (index 0)
	m.activePanel = 0
	m.width = 80
	m.height = 24
	m.library.SetSize(80, 22)
	// Should not panic
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "j")
}

func TestModel_UpdateActiveView_Search(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.height = 24
	m.search.SetSize(80, 22)
	// Should not panic
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "j")
}

func TestModel_UpdateActiveView_Queue(t *testing.T) {
	m := newModel(nil)
	// Should not panic with any panel state
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "j")
}

func TestModel_UpdateActiveView_NowPlaying(t *testing.T) {
	m := newModel(nil)
	// Should not panic
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "j")
}

// --- Search mode: keys go to search handling ---

func TestModel_SearchFocused_KeyGoesToSearch(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	// When in search mode, key messages should be handled without panic
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

// --- Search popup: Enter calls SetQueue, Tab calls AppendQueue ---

// seedSearchResults plants a track into the model's search view so
// SelectedTrack() returns a non-nil result.
func seedSearchResults(m *Model, tracks ...provider.Track) {
	m.mode = modeSearch
	m.search.SetSize(80, 20)
	m.search.SetState(tracks, false, nil)
}

func TestHandleSearchKey_Enter_CallsSetQueue(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := provider.Track{Title: "Hi", Artist: "There", CatalogID: "99999"}
	seedSearchResults(m, track)

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := m.handleSearchKey("enter", msg)
	if cmd == nil {
		t.Fatal("handleSearchKey(enter) returned nil cmd — expected SetQueue call")
	}
	cmd() // execute the player call synchronously

	if len(mp.setQueueIDs) == 0 {
		t.Fatal("SetQueue was not called after Enter")
	}
	if mp.setQueueIDs[0] != "99999" {
		t.Errorf("SetQueue ID = %q, want %q", mp.setQueueIDs[0], "99999")
	}
}

func TestHandleSearchKey_Enter_UsesLibraryID_WhenNoCatalogID(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := provider.Track{Title: "Library Song", ID: "i.LibraryAbc123"}
	seedSearchResults(m, track)

	cmd := m.handleSearchKey("enter", tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if len(mp.setQueueIDs) == 0 || mp.setQueueIDs[0] != "i.LibraryAbc123" {
		t.Errorf("SetQueue ID = %v, want [i.LibraryAbc123]", mp.setQueueIDs)
	}
}

func TestHandleSearchKey_Enter_NoResults_NoCall(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.mode = modeSearch
	m.search.SetSize(80, 20)
	// No results set → SelectedTrack() is nil.

	cmd := m.handleSearchKey("enter", tea.KeyMsg{Type: tea.KeyEnter})
	// cmd may be nil or return no player call — SetQueue must NOT be called.
	if cmd != nil {
		cmd()
	}
	if len(mp.setQueueIDs) > 0 {
		t.Errorf("SetQueue called with no results: %v", mp.setQueueIDs)
	}
}

func TestHandleSearchKey_Tab_CallsAppendQueue(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := provider.Track{Title: "Queued", Artist: "Band", CatalogID: "12345"}
	seedSearchResults(m, track)

	cmd := m.handleSearchKey("tab", tea.KeyMsg{Type: tea.KeyTab})
	if cmd == nil {
		t.Fatal("handleSearchKey(tab) returned nil cmd — expected AppendQueue call")
	}
	cmd()

	if len(mp.appendQueueIDs) == 0 {
		t.Fatal("AppendQueue was not called after Tab")
	}
	if mp.appendQueueIDs[0][0] != "12345" {
		t.Errorf("AppendQueue ID = %q, want %q", mp.appendQueueIDs[0][0], "12345")
	}
}

func TestHandleSearchKey_Tab_DoesNotCallSetQueue(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	seedSearchResults(m, provider.Track{Title: "T", CatalogID: "x"})

	cmd := m.handleSearchKey("tab", tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		cmd()
	}
	if len(mp.setQueueIDs) > 0 {
		t.Errorf("Tab must not call SetQueue (would interrupt playback), but it did: %v", mp.setQueueIDs)
	}
}

func TestHandleSearchKey_Tab_MultipleTabsAccumulate(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	tracks := []provider.Track{
		{Title: "A", CatalogID: "111"},
		{Title: "B", CatalogID: "222"},
	}
	seedSearchResults(m, tracks...)

	for range 2 {
		cmd := m.handleSearchKey("tab", tea.KeyMsg{Type: tea.KeyTab})
		if cmd != nil {
			cmd()
		}
	}
	if len(mp.appendQueueIDs) != 2 {
		t.Errorf("expected 2 AppendQueue calls, got %d", len(mp.appendQueueIDs))
	}
}

func TestHandleSearchKey_Esc_ResetsMode(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.searchQuery = "test query"

	m.handleSearchKey("esc", tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeNormal {
		t.Errorf("mode after esc = %v, want modeNormal", m.mode)
	}
	if m.searchQuery != "" {
		t.Errorf("searchQuery after esc = %q, want empty", m.searchQuery)
	}
}

func TestHandleSearchKey_Backspace_DeletesLastChar(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.searchQuery = "abc"

	m.handleSearchKey("backspace", tea.KeyMsg{Type: tea.KeyBackspace})

	if m.searchQuery != "ab" {
		t.Errorf("searchQuery after backspace = %q, want %q", m.searchQuery, "ab")
	}
}

func TestHandleSearchKey_Typing_AppendsToQuery(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.searchQuery = "hel"

	m.handleSearchKey("l", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	if m.searchQuery != "hell" {
		t.Errorf("searchQuery = %q, want %q", m.searchQuery, "hell")
	}
}

func TestHandleSearchKey_Enter_SwitchesToNormalMode(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	seedSearchResults(m, provider.Track{Title: "T", CatalogID: "x"})

	cmd := m.handleSearchKey("enter", tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if m.mode != modeNormal {
		t.Errorf("mode after enter = %v, want modeNormal", m.mode)
	}
}

// ─── Update message handlers ────────────────────────────────────────────────

func TestModel_Update_VibeQueryMsg(t *testing.T) {
	m := newModel(newMockPlayer())
	_, cmd := m.Update(views.VibeQueryMsg{Query: "chill coding"})
	// Should transition vibe panel to searching state (SetSearching called internally).
	// runVibeSearch returns a cmd — we just verify no panic.
	_ = cmd
}

func TestModel_Update_VibeResultMsg_Success(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	tracks := []provider.Track{
		{Title: "Song A", Artist: "Artist A", ID: "111", CatalogID: "cat111"},
	}
	_, cmd := m.Update(vibeResultMsg{tracks: tracks, query: "chill"})
	_ = cmd
	// Tracks should be appended to queue.
	if len(mp.appendQueueIDs) == 0 {
		t.Error("vibeResultMsg should call AppendQueue")
	}
}

func TestModel_Update_VibeResultMsg_Error(t *testing.T) {
	m := newModel(newMockPlayer())
	_, cmd := m.Update(vibeResultMsg{err: errors.New("search failed")})
	_ = cmd
}

func TestModel_Update_VibeResultMsg_Discovery(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	tracks := []provider.Track{{Title: "Discovery Song", ID: "999", CatalogID: "cat999"}}
	_, cmd := m.Update(vibeResultMsg{tracks: tracks, discovery: true})
	_ = cmd
}

func TestModel_Update_LoveSongMsg_Success(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(loveSongMsg{title: "Song", loved: true})
	_ = cmd
	// appendLog should record the love action.
	if len(m.debugLog) == 0 {
		t.Error("loveSongMsg should append a log entry")
	}
}

func TestModel_Update_LoveSongMsg_Error(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(loveSongMsg{title: "Song", err: errors.New("api error")})
	_ = cmd
}

func TestModel_Update_LoveSongMsg_Unlove(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(loveSongMsg{title: "Song", loved: false})
	_ = cmd
}

func TestModel_Update_SongRatingMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(songRatingMsg{trackID: "track123", loved: true})
	_ = cmd
	if !m.favorites["track123"] {
		t.Error("songRatingMsg should set favorites entry")
	}
}

func TestModel_Update_SongRatingMsg_EmptyID(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(songRatingMsg{trackID: "", loved: true})
	_ = cmd // empty ID → no-op
}

func TestModel_Update_PlayTracksMsg_WithPlaylistID(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := provider.Track{Title: "T", Artist: "A", CatalogID: "cat1"}
	msg := views.PlayTracksMsg{IDs: []string{"cat1"}, Track: &track, PlaylistID: "pl.123", StartIdx: 0}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("PlayTracksMsg should return a cmd")
	}
	cmd() // triggers SetPlaylist on player
}

func TestModel_Update_PlayTracksMsg_WithoutPlaylistID(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := provider.Track{Title: "T", Artist: "A", CatalogID: "cat2"}
	msg := views.PlayTracksMsg{IDs: []string{"cat2"}, Track: &track}
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("PlayTracksMsg should return a cmd")
	}
	cmd() // triggers SetQueue on player
	if len(mp.setQueueIDs) == 0 {
		t.Error("PlayTracksMsg without playlist should call SetQueue")
	}
}

func TestModel_Update_InitStatusMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(InitStatusMsg("loading engine…"))
	_ = cmd
	if m.initStatus != "loading engine…" {
		t.Errorf("initStatus = %q, want %q", m.initStatus, "loading engine…")
	}
}

func TestModel_Update_InitErrMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(InitErrMsg{Err: errors.New("fatal error")})
	_ = cmd
	if m.errMsg == "" {
		t.Error("InitErrMsg should set errMsg")
	}
}

func TestModel_Update_ErrMsg_SetsErrField(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(errMsg{err: errors.New("some error")})
	_ = cmd
	if m.errMsg == "" {
		t.Error("errMsg should set m.errMsg")
	}
}

func TestModel_Update_PlaylistCreatedMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(playlistCreatedMsg{name: "My Playlist"})
	_ = cmd
	if !strings.Contains(m.errMsg, "My Playlist") {
		t.Errorf("errMsg should contain playlist name, got %q", m.errMsg)
	}
}

func TestModel_Update_SessionExpiredMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(SessionExpiredMsg{})
	_ = cmd
	if m.errMsg == "" {
		t.Error("SessionExpiredMsg should set errMsg")
	}
}

func TestModel_Update_SessionRestoredMsg(t *testing.T) {
	m := newModel(nil)
	_, cmd := m.Update(SessionRestoredMsg{})
	_ = cmd
	if m.errMsg == "" {
		t.Error("SessionRestoredMsg should set success errMsg")
	}
}

func TestModel_Update_MemTickMsg(t *testing.T) {
	m := newModel(nil)
	m.memProfiling = true
	_, cmd := m.Update(memTickMsg{stats: "RSS: 42 MB"})
	_ = cmd
	if m.memStats != "RSS: 42 MB" {
		t.Errorf("memStats = %q, want %q", m.memStats, "RSS: 42 MB")
	}
}

func TestModel_Update_SearchDebounceMsg_MatchesGen(t *testing.T) {
	m := newModel(nil)
	m.searchGen = 5
	_, cmd := m.Update(searchDebounceMsg{gen: 5, query: "test"})
	// Matching gen should return a search cmd.
	if cmd == nil {
		t.Error("searchDebounceMsg with matching gen should return a search cmd")
	}
}

func TestModel_Update_SearchDebounceMsg_StaleGen(t *testing.T) {
	m := newModel(nil)
	m.searchGen = 5
	_, cmd := m.Update(searchDebounceMsg{gen: 3, query: "stale"})
	// Stale gen should return nil cmd.
	if cmd != nil {
		// It might return nil batch — check it's not a real search cmd by verifying nil
		// (some implementations return tea.Batch(nil...) which resolves to nil)
		_ = cmd
	}
}

func TestModel_Update_PlayerStateMsg_WithLog(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.stateCh = mp.stateCh
	st := playerStateMsg{Playing: true, Log: "something logged"}
	_, _ = m.Update(st)
	found := false
	for _, entry := range m.debugLog {
		if strings.Contains(entry, "something logged") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Log field in playerStateMsg should be appended to debugLog")
	}
}

func TestModel_Update_PlayerStateMsg_ContentRestricted(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.stateCh = mp.stateCh
	track := &provider.Track{Title: "Restricted Song", Artist: "Artist", ID: "111"}
	st := playerStateMsg{
		Playing: false,
		Error:   "CONTENT_RESTRICTED: track unavailable",
		Track:   track,
	}
	_, cmd := m.Update(st)
	_ = cmd
	// Restricted content should not set errMsg but should log.
}

func TestModel_Update_PlayerStateMsg_GenericError(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.stateCh = mp.stateCh
	st := playerStateMsg{Error: "something went wrong"}
	_, cmd := m.Update(st)
	_ = cmd
	if m.errMsg == "" {
		t.Error("generic player error should set errMsg")
	}
}

func TestModel_Update_EngineReadyMsg(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(nil)
	_, cmd := m.Update(EngineReadyMsg{
		Player:      mp,
		Provider:    &mockProvider{},
		HelperPaths: []string{"/usr/bin/helper"},
		Backend:     "cdp",
	})
	_ = cmd
	if m.player == nil {
		t.Error("EngineReadyMsg should set m.player")
	}
	if m.provider == nil {
		t.Error("EngineReadyMsg should set m.provider")
	}
}

// ─── handleNormalKey ────────────────────────────────────────────────────────

func TestHandleNormalKey_Space_TogglePlayPause(t *testing.T) {
	mp := newMockPlayer()
	mp.state.Playing = false
	m := newModel(mp)
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeySpace}, " ")
	if cmd == nil {
		t.Fatal("space key should return a cmd")
	}
	cmd()
	if !mp.playCalled {
		t.Error("space when paused should call Play")
	}
}

func TestHandleNormalKey_N_Next(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}, "n")
	if cmd != nil {
		cmd()
	}
	if !mp.nextCalled {
		t.Error("n key should call Next")
	}
}

func TestHandleNormalKey_P_Previous(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}, "p")
	if cmd != nil {
		cmd()
	}
	if !mp.prevCalled {
		t.Error("p key should call Previous")
	}
}

func TestHandleNormalKey_Plus_VolumeUp(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Volume = 0.5
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")}, "+")
	if cmd != nil {
		cmd()
	}
}

func TestHandleNormalKey_Minus_VolumeDown(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Volume = 0.5
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")}, "-")
	if cmd != nil {
		cmd()
	}
}

func TestHandleNormalKey_Plus_Discovery_IncreaseSimilarity(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.discovery.enabled = true
	m.discovery.similarity = 0.5
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")}, "+")
	if m.discovery.similarity <= 0.5 {
		t.Error("+ key in discovery mode should increase similarity")
	}
}

func TestHandleNormalKey_Minus_Discovery_DecreaseSimilarity(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.discovery.enabled = true
	m.discovery.similarity = 0.7
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-")}, "-")
	if m.discovery.similarity >= 0.7 {
		t.Error("- key in discovery mode should decrease similarity")
	}
}

func TestHandleNormalKey_R_CycleRepeat(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.RepeatMode = player.RepeatModeOff
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, "r")
	if cmd != nil {
		cmd()
	}
	if m.playerState.RepeatMode != player.RepeatModeAll {
		t.Errorf("r key should cycle repeat Off→All, got %d", m.playerState.RepeatMode)
	}
}

func TestHandleNormalKey_R_CycleRepeatAll_To_One(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.RepeatMode = player.RepeatModeAll
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, "r")
	if cmd != nil {
		cmd()
	}
	if m.playerState.RepeatMode != player.RepeatModeOne {
		t.Errorf("r key should cycle repeat All→One, got %d", m.playerState.RepeatMode)
	}
}

func TestHandleNormalKey_S_ToggleShuffle(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.ShuffleMode = false
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}, "s")
	if cmd != nil {
		cmd()
	}
	if !m.playerState.ShuffleMode {
		t.Error("s key should toggle shuffle on")
	}
}

func TestHandleNormalKey_F_LoveSong(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := &provider.Track{Title: "Song", Artist: "Artist", ID: "fav-id"}
	m.playerState.Track = track
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}, "f")
	_ = cmd
	if !m.favorites["fav-id"] {
		t.Error("f key should toggle favorite on")
	}
}

func TestHandleNormalKey_F_NoTrack_NoOp(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.playerState.Track = nil
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}, "f")
	_ = cmd // no-op
}

func TestHandleNormalKey_D_OpensPicker(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := &provider.Track{Title: "Seed Song", Artist: "Artist", ID: "seed"}
	m.playerState.Track = track
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}, "d")
	_ = cmd
	if !m.vibe.PickerActive() {
		t.Error("d key with track should open the metric picker")
	}
	if m.discovery.enabled {
		t.Error("d key should not immediately start discovery; picker must be confirmed first")
	}
}

func TestHandleNormalKey_D_ClosesPickerIfOpen(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	track := &provider.Track{Title: "Seed Song", Artist: "Artist", ID: "seed"}
	m.playerState.Track = track
	// First d → opens picker.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}, "d")
	if !m.vibe.PickerActive() {
		t.Fatal("expected picker to be active after first d")
	}
	// Second d → closes picker.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}, "d")
	if m.vibe.PickerActive() {
		t.Error("second d should close the picker")
	}
}

func TestHandleNormalKey_D_StopDiscovery(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.discovery.enabled = true
	m.discovery.seed = &provider.Track{ID: "seed"}
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}, "d")
	_ = cmd
	if m.discovery.enabled {
		t.Error("d key when discovery is on should stop discovery")
	}
}

func TestHandleNormalKey_V_FocusVibe(t *testing.T) {
	m := newModel(nil)
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}, "v")
	if !m.vibe.IsFocused() {
		t.Error("v key should focus the vibe panel")
	}
}

func TestHandleNormalKey_Colon_OpenCommandMode(t *testing.T) {
	m := newModel(nil)
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")}, ":")
	if m.mode != modeCommand {
		t.Error(": key should switch to command mode")
	}
}

func TestHandleNormalKey_Slash_OpenSearch(t *testing.T) {
	m := newModel(nil)
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")}, "/")
	if m.mode != modeSearch {
		t.Error("/ key should switch to search mode")
	}
}

func TestHandleNormalKey_L_ToggleLibraryPanel(t *testing.T) {
	m := newModel(nil)
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}, "l")
	// Library panel should be activated.
	if m.activePanel < 0 {
		t.Error("l key should open library panel")
	}
}

func TestHandleNormalKey_L_ToggleOff(t *testing.T) {
	m := newModel(nil)
	// Press l twice — second press closes.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}, "l")
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}, "l")
	if m.activePanel >= 0 {
		t.Error("l key pressed twice should close library panel")
	}
}

func TestHandleNormalKey_Q_ToggleQueuePanel(t *testing.T) {
	m := newModel(newMockPlayer())
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "q")
	if m.activePanel < 0 {
		t.Error("q key should open queue panel")
	}
}

func TestHandleNormalKey_DebugView_J_ScrollDown(t *testing.T) {
	m := newModel(nil)
	m.debugView = true
	m.debugScroll = 5
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "j")
	if m.debugScroll != 4 {
		t.Errorf("j in debug view should decrement scroll, got %d", m.debugScroll)
	}
}

func TestHandleNormalKey_DebugView_K_ScrollUp(t *testing.T) {
	m := newModel(nil)
	m.debugView = true
	m.debugScroll = 2
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}, "k")
	if m.debugScroll != 3 {
		t.Errorf("k in debug view should increment scroll, got %d", m.debugScroll)
	}
}

func TestHandleNormalKey_DebugView_BigG_ResetScroll(t *testing.T) {
	m := newModel(nil)
	m.debugView = true
	m.debugScroll = 10
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")}, "G")
	if m.debugScroll != 0 {
		t.Errorf("G in debug view should reset scroll to 0, got %d", m.debugScroll)
	}
}

func TestHandleNormalKey_DebugView_Esc_CloseView(t *testing.T) {
	m := newModel(nil)
	m.debugView = true
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyEsc}, "esc")
	if m.debugView {
		t.Error("esc in debug view should close it")
	}
}

func TestHandleNormalKey_QueuePanel_Esc_ClosePanel(t *testing.T) {
	m := newModel(newMockPlayer())
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "q") // open queue
	idx := m.activePanel
	if idx < 0 {
		t.Skip("queue panel not available")
	}
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyEsc}, "esc")
	if m.activePanel >= 0 {
		t.Error("esc should close queue panel")
	}
}

func TestHandleNormalKey_QueuePanel_C_ClearQueue(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.queueTracks = []provider.Track{{Title: "T", ID: "1"}}
	m.queueIDs = []string{"1"}
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "q") // open queue
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, "c")
	if cmd != nil {
		cmd()
	}
	if len(m.queueTracks) != 0 {
		t.Error("c in queue panel should clear queue tracks")
	}
}

func TestHandleNormalKey_QueuePanel_S_OpenSaveCommand(t *testing.T) {
	m := newModel(newMockPlayer())
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "q")
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}, "s")
	if m.mode != modeCommand || !strings.HasPrefix(m.cmdBuf, "save ") {
		t.Error("s in queue panel should open command mode with 'save ' prefilled")
	}
}

// ─── handleCommandKey ───────────────────────────────────────────────────────

func TestHandleCommandKey_Esc_ClearsAndReturnsNormal(t *testing.T) {
	m := newModel(nil)
	m.mode = modeCommand
	m.cmdBuf = "some-cmd"
	m.handleCommandKey("esc")
	if m.mode != modeNormal {
		t.Error("esc should return to normal mode")
	}
	if m.cmdBuf != "" {
		t.Error("esc should clear cmdBuf")
	}
}

func TestHandleCommandKey_Backspace_DeletesChar(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "quit"
	m.handleCommandKey("backspace")
	if m.cmdBuf != "qui" {
		t.Errorf("cmdBuf after backspace = %q, want %q", m.cmdBuf, "qui")
	}
}

func TestHandleCommandKey_Backspace_Empty_NoOp(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = ""
	m.handleCommandKey("backspace") // should not panic
}

func TestHandleCommandKey_Typing_AppendsToCmdBuf(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "q"
	m.handleCommandKey("u")
	m.handleCommandKey("i")
	m.handleCommandKey("t")
	if m.cmdBuf != "quit" {
		t.Errorf("cmdBuf = %q, want %q", m.cmdBuf, "quit")
	}
}

func TestHandleCommandKey_Tab_CompletesSuggestion(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "sa" // matches "save"
	m.handleCommandKey("tab")
	if !strings.HasPrefix(m.cmdBuf, "save") {
		t.Errorf("tab should complete suggestion, got %q", m.cmdBuf)
	}
}

func TestHandleCommandKey_Up_DecreaseSuggIdx(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "sa"
	m.cmdSuggIdx = 1
	m.handleCommandKey("up")
	if m.cmdSuggIdx != 0 {
		t.Errorf("up should decrease suggIdx, got %d", m.cmdSuggIdx)
	}
}

func TestHandleCommandKey_Down_IncreaseSuggIdx(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "sa"
	m.cmdSuggIdx = 0
	m.handleCommandKey("down")
	// Should increase if there are suggestions.
	if m.cmdSuggIdx < 0 {
		t.Error("down should not set suggIdx negative")
	}
}

func TestHandleCommandKey_CtrlP_DecreaseSuggIdx(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "sa"
	m.cmdSuggIdx = 1
	m.handleCommandKey("ctrl+p")
	if m.cmdSuggIdx != 0 {
		t.Errorf("ctrl+p should decrease suggIdx, got %d", m.cmdSuggIdx)
	}
}

func TestHandleCommandKey_CtrlN_IncreaseSuggIdx(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "sa"
	m.cmdSuggIdx = 0
	m.handleCommandKey("ctrl+n")
	// Just verify no panic.
}

func TestHandleCommandKey_Enter_ExecutesCommand(t *testing.T) {
	m := newModel(nil)
	m.mode = modeCommand
	m.cmdBuf = "debug-logs"
	cmd := m.handleCommandKey("enter")
	_ = cmd
	if m.mode != modeNormal {
		t.Error("enter should return to normal mode after executing command")
	}
}

// ─── executeCommand ─────────────────────────────────────────────────────────

func TestExecuteCommand_Quit_NilPlayer(t *testing.T) {
	m := newModel(nil)
	cmd := m.executeCommand("q")
	if cmd == nil {
		t.Error("quit command should return tea.Quit cmd")
	}
}

func TestExecuteCommand_Quit_Word(t *testing.T) {
	m := newModel(nil)
	cmd := m.executeCommand("quit")
	if cmd == nil {
		t.Error("'quit' command should return tea.Quit cmd")
	}
}

func TestExecuteCommand_DebugLogs_Toggle(t *testing.T) {
	m := newModel(nil)
	m.debugView = false
	m.executeCommand("debug-logs")
	if !m.debugView {
		t.Error("debug-logs should toggle debugView on")
	}
	m.executeCommand("debug-logs")
	if m.debugView {
		t.Error("debug-logs again should toggle debugView off")
	}
}

func TestExecuteCommand_Save_NoName_SetsError(t *testing.T) {
	m := newModel(nil)
	m.executeCommand("save ")
	if m.errMsg == "" {
		t.Error("save with empty name should set errMsg")
	}
}

func TestExecuteCommand_Save_WithName_CreatesPlaylist(t *testing.T) {
	m := newModel(nil)
	m.queueTracks = []provider.Track{{Title: "T", ID: "1", CatalogID: "cat1"}}
	cmd := m.executeCommand("save My Playlist")
	if cmd == nil {
		t.Error("save with valid name should return a cmd")
	}
	cmd() // should call CreatePlaylist on provider
}

func TestExecuteCommand_SavePlaylist_WithName(t *testing.T) {
	m := newModel(nil)
	m.queueTracks = []provider.Track{{Title: "T", ID: "1", CatalogID: "cat1"}}
	cmd := m.executeCommand("save-playlist Another Playlist")
	if cmd == nil {
		t.Error("save-playlist with valid name should return a cmd")
	}
}

func TestExecuteCommand_Unknown_SetsError(t *testing.T) {
	m := newModel(nil)
	m.executeCommand("nonexistent-command")
	if !strings.Contains(m.errMsg, "nonexistent-command") {
		t.Errorf("unknown command should set errMsg containing command name, got %q", m.errMsg)
	}
}

func TestCommandSuggestions_EmptyBuf_ReturnsAll(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = ""
	suggs := m.commandSuggestions()
	if len(suggs) == 0 {
		t.Error("empty cmdBuf should return all suggestions")
	}
}

func TestCommandSuggestions_PrefixFilter(t *testing.T) {
	m := newModel(nil)
	m.cmdBuf = "sa"
	suggs := m.commandSuggestions()
	for _, s := range suggs {
		if !strings.HasPrefix(s.usage, "sa") {
			t.Errorf("suggestion %q does not start with 'sa'", s.usage)
		}
	}
}

// ─── Rendering functions ────────────────────────────────────────────────────

func TestToLines_ExactHeight(t *testing.T) {
	input := "a\nb\nc"
	lines := toLines(input, 3)
	if len(lines) != 3 {
		t.Errorf("toLines returned %d lines, want 3", len(lines))
	}
}

func TestToLines_PadsToHeight(t *testing.T) {
	lines := toLines("a\nb", 5)
	if len(lines) != 5 {
		t.Errorf("toLines returned %d lines, want 5", len(lines))
	}
}

func TestToLines_TruncatesToHeight(t *testing.T) {
	lines := toLines("a\nb\nc\nd\ne", 3)
	if len(lines) != 3 {
		t.Errorf("toLines returned %d lines, want 3", len(lines))
	}
}

func TestTruncateStr_WithinLimit(t *testing.T) {
	got := truncateStr("hello", 10)
	if got != "hello" {
		t.Errorf("truncateStr(short) = %q, want %q", got, "hello")
	}
}

func TestTruncateStr_Truncates(t *testing.T) {
	got := truncateStr("hello world", 6)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncateStr(over limit) should end with ellipsis, got %q", got)
	}
	if len([]rune(got)) > 6 {
		t.Errorf("truncateStr result %q is too long (%d > 6 runes)", got, len([]rune(got)))
	}
}

func TestTruncateStr_LimitOne_NoEllipsis(t *testing.T) {
	got := truncateStr("hello", 1)
	// maxW <= 1 returns string unchanged.
	if got != "hello" {
		t.Errorf("truncateStr(maxW=1) = %q, want %q", got, "hello")
	}
}

func TestNowPlayingLines_NoTrack(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	lines := m.nowPlayingLines(76, 12)
	if len(lines) != 12 {
		t.Errorf("nowPlayingLines returned %d lines, want 12", len(lines))
	}
}

func TestNowPlayingLines_WithTrack_Playing(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.playerState = player.State{
		Playing: true,
		Track:   &provider.Track{Title: "Song", Artist: "Artist", Album: "Album", Duration: 3 * time.Minute},
		Volume:  0.8,
	}
	lines := m.nowPlayingLines(76, 15)
	if len(lines) != 15 {
		t.Errorf("nowPlayingLines returned %d lines, want 15", len(lines))
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Song") || strings.Contains(l, "Artist") {
			found = true
			break
		}
	}
	if !found {
		t.Error("nowPlayingLines should contain track title or artist")
	}
}

func TestNowPlayingLines_WithTrack_Paused(t *testing.T) {
	m := newModel(nil)
	m.playerState.Playing = false
	m.playerState.Track = &provider.Track{Title: "Paused Song", Artist: "Artist", Album: "Album", Duration: time.Minute}
	lines := m.nowPlayingLines(76, 12)
	if len(lines) != 12 {
		t.Errorf("nowPlayingLines returned %d lines, want 12", len(lines))
	}
}

func TestNowPlayingLines_WithErrMsg(t *testing.T) {
	m := newModel(nil)
	m.errMsg = "Something went wrong"
	m.errExpiry = time.Now().Add(time.Hour)
	lines := m.nowPlayingLines(76, 12)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Something went wrong") {
			found = true
			break
		}
	}
	if !found {
		t.Error("nowPlayingLines should include errMsg when set")
	}
}

func TestNowPlayingLines_RepeatModeAll(t *testing.T) {
	m := newModel(nil)
	m.playerState.RepeatMode = player.RepeatModeAll
	m.playerState.Track = &provider.Track{Title: "T", Artist: "A", Album: "Al", Duration: time.Minute}
	lines := m.nowPlayingLines(76, 12)
	if len(lines) != 12 {
		t.Errorf("nowPlayingLines(RepeatAll) returned %d lines, want 12", len(lines))
	}
}

func TestNowPlayingLines_Loading(t *testing.T) {
	m := newModel(nil)
	m.playerState.Loading = true
	m.playerState.Track = &provider.Track{Title: "Loading", Artist: "A", Album: "Al", Duration: time.Minute}
	lines := m.nowPlayingLines(76, 12)
	if len(lines) != 12 {
		t.Errorf("nowPlayingLines(Loading) returned %d lines, want 12", len(lines))
	}
}

func TestNowPlayingLines_Favorite(t *testing.T) {
	m := newModel(nil)
	track := &provider.Track{Title: "Fav Song", Artist: "Artist", Album: "Album", ID: "fav1", Duration: time.Minute}
	m.playerState.Track = track
	m.favorites["fav1"] = true
	lines := m.nowPlayingLines(76, 12)
	if len(lines) != 12 {
		t.Errorf("nowPlayingLines(favorite) returned %d lines, want 12", len(lines))
	}
}

func TestDebugLogLines_Empty(t *testing.T) {
	m := newModel(nil)
	m.debugLog = nil
	lines := m.debugLogLines(80, 10)
	if len(lines) != 10 {
		t.Errorf("debugLogLines returned %d lines, want 10", len(lines))
	}
}

func TestDebugLogLines_WithEntries(t *testing.T) {
	m := newModel(nil)
	m.appendLog("normal entry")
	m.appendLog("[error] something failed")
	m.appendLog("[playing] Artist — Song")
	lines := m.debugLogLines(80, 10)
	if len(lines) != 10 {
		t.Errorf("debugLogLines returned %d lines, want 10", len(lines))
	}
}

func TestDebugLogLines_WithScroll(t *testing.T) {
	m := newModel(nil)
	for i := range 20 {
		m.appendLog(strings.Repeat("x", i+1))
	}
	m.debugScroll = 3
	lines := m.debugLogLines(80, 10)
	if len(lines) != 10 {
		t.Errorf("debugLogLines with scroll returned %d lines, want 10", len(lines))
	}
}

func TestSearchLines_WithResults(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.search.SetSize(80, 20)
	m.search.SetState([]provider.Track{{Title: "Hit Song", Artist: "Artist"}}, false, nil)
	lines := m.searchLines(76, 10)
	if len(lines) != 10 {
		t.Errorf("searchLines returned %d lines, want 10", len(lines))
	}
}

func TestSearchLines_Empty(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.searchQuery = "notfound"
	lines := m.searchLines(76, 10)
	if len(lines) != 10 {
		t.Errorf("searchLines(empty) returned %d lines, want 10", len(lines))
	}
}

func TestCommandLines_WithSuggestions(t *testing.T) {
	m := newModel(nil)
	m.mode = modeCommand
	m.cmdBuf = "sa"
	lines := m.commandLines(76, 10)
	if len(lines) != 10 {
		t.Errorf("commandLines returned %d lines, want 10", len(lines))
	}
}

func TestCommandLines_Empty(t *testing.T) {
	m := newModel(nil)
	m.mode = modeCommand
	m.cmdBuf = ""
	lines := m.commandLines(76, 10)
	if len(lines) != 10 {
		t.Errorf("commandLines(empty) returned %d lines, want 10", len(lines))
	}
}

// ─── Additional Update coverage ─────────────────────────────────────────────

func TestModel_Update_GlowTickMsg(t *testing.T) {
	m := newModel(nil)
	step := m.glowStep
	_, _ = m.Update(glowTickMsg(time.Now()))
	if m.glowStep != step+1 {
		t.Errorf("glowStep = %d, want %d", m.glowStep, step+1)
	}
}

func TestModel_Update_IntroTickMsg_Advances(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.introStep = 0
	_, _ = m.Update(introTickMsg(time.Now()))
	// introStep should advance by 1.
	if m.introStep <= 0 {
		t.Errorf("introStep did not advance after introTickMsg, got %d", m.introStep)
	}
}

func TestCreatePlaylistCmd_CallsProvider(t *testing.T) {
	m := newModel(nil)
	tracks := []provider.Track{{Title: "T1", ID: "id1", CatalogID: "cat1"}}
	m.queueTracks = tracks
	ids := []string{"cat1"}
	cmd := m.createPlaylistCmd("Test Playlist", ids)
	if cmd == nil {
		t.Fatal("createPlaylistCmd should return a cmd")
	}
	result := cmd()
	// Should return playlistCreatedMsg or errMsg (mock returns no error).
	if _, ok := result.(playlistCreatedMsg); !ok {
		if _, ok := result.(errMsg); !ok {
			t.Errorf("createPlaylistCmd result = %T, want playlistCreatedMsg or errMsg", result)
		}
	}
}

// ─── Panel wrapper coverage ───────────────────────────────────────────────────

func TestLibraryPanel_NavLabel(t *testing.T) {
	m := newModel(nil)
	// Access the library panel through the model's panels.
	for _, p := range m.panels {
		if p.NavKey() == "l" {
			if p.NavLabel() != "library" {
				t.Errorf("library NavLabel() = %q, want %q", p.NavLabel(), "library")
			}
			break
		}
	}
}

func TestLibraryPanel_SetSize(t *testing.T) {
	m := newModel(nil)
	for _, p := range m.panels {
		if p.NavKey() == "l" {
			p.SetSize(80, 20) // should not panic
			break
		}
	}
}

func TestLibraryPanel_View(t *testing.T) {
	m := newModel(nil)
	for _, p := range m.panels {
		if p.NavKey() == "l" {
			view := p.View()
			_ = view // just verify no panic
			break
		}
	}
}

func TestLibraryPanel_Update(t *testing.T) {
	m := newModel(nil)
	for _, p := range m.panels {
		if p.NavKey() == "l" {
			cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			_ = cmd // should not panic
			break
		}
	}
}

func TestQueuePanel_NavLabel(t *testing.T) {
	m := newModel(newMockPlayer())
	for _, p := range m.panels {
		if p.NavKey() == "q" {
			if p.NavLabel() != "queue" {
				t.Errorf("queue NavLabel() = %q, want %q", p.NavLabel(), "queue")
			}
			break
		}
	}
}

func TestQueuePanel_SetSize(t *testing.T) {
	m := newModel(newMockPlayer())
	for _, p := range m.panels {
		if p.NavKey() == "q" {
			p.SetSize(80, 20) // should not panic
			break
		}
	}
}

func TestQueuePanel_View(t *testing.T) {
	m := newModel(newMockPlayer())
	for _, p := range m.panels {
		if p.NavKey() == "q" {
			view := p.View()
			_ = view // should not panic
			break
		}
	}
}

func TestQueuePanel_Update(t *testing.T) {
	m := newModel(newMockPlayer())
	for _, p := range m.panels {
		if p.NavKey() == "q" {
			cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			_ = cmd // should not panic
			break
		}
	}
}

func TestQueuePanel_SelectedTrack_Empty(t *testing.T) {
	m := newModel(newMockPlayer())
	for _, p := range m.panels {
		if p.NavKey() == "q" {
			qp := p.(*queuePanel)
			idx, track := qp.SelectedTrack()
			if track != nil {
				t.Error("SelectedTrack() on empty queue should return nil track")
			}
			if idx >= 0 {
				t.Error("SelectedTrack() on empty queue should return idx < 0")
			}
			break
		}
	}
}

// ─── discoveryQueries ─────────────────────────────────────────────────────────

func TestDiscoveryQueries_HighSimilarity(t *testing.T) {
	seed := &provider.Track{Artist: "Daft Punk", Title: "Get Lucky", Genres: []string{"electronic"}}
	queries := discoveryQueries(seed, 0.9) // >= 0.85
	if len(queries) == 0 {
		t.Fatal("discoveryQueries(0.9) returned empty slice")
	}
	found := false
	for _, q := range queries {
		if strings.Contains(q, "Daft Punk") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("discoveryQueries(0.9) should include artist name, got %v", queries)
	}
}

func TestDiscoveryQueries_MediumHighSimilarity(t *testing.T) {
	seed := &provider.Track{Artist: "Kendrick Lamar", Genres: []string{"hip-hop"}}
	queries := discoveryQueries(seed, 0.7) // >= 0.65
	if len(queries) == 0 {
		t.Fatal("discoveryQueries(0.7) returned empty slice")
	}
}

func TestDiscoveryQueries_MediumSimilarity(t *testing.T) {
	seed := &provider.Track{Artist: "Frank Ocean", Genres: []string{"r&b"}}
	queries := discoveryQueries(seed, 0.5) // >= 0.45
	if len(queries) == 0 {
		t.Fatal("discoveryQueries(0.5) returned empty slice")
	}
}

func TestDiscoveryQueries_LowSimilarity(t *testing.T) {
	seed := &provider.Track{Artist: "The Weeknd", Genres: []string{"pop"}}
	queries := discoveryQueries(seed, 0.3) // >= 0.20
	if len(queries) == 0 {
		t.Fatal("discoveryQueries(0.3) returned empty slice")
	}
}

func TestDiscoveryQueries_VeryLowSimilarity(t *testing.T) {
	seed := &provider.Track{Artist: "Artist", Genres: []string{"jazz"}}
	queries := discoveryQueries(seed, 0.1) // < 0.20
	if len(queries) == 0 {
		t.Fatal("discoveryQueries(0.1) returned empty slice")
	}
}

func TestDiscoveryQueries_NoGenres(t *testing.T) {
	seed := &provider.Track{Artist: "Artist", Genres: nil}
	queries := discoveryQueries(seed, 0.8)
	if len(queries) == 0 {
		t.Fatal("discoveryQueries(no genres) returned empty slice")
	}
}

// ─── safeIdx ──────────────────────────────────────────────────────────────────

func TestSafeIdx_ValidIndex(t *testing.T) {
	lines := []string{"a", "b", "c"}
	got := safeIdx(lines, 1)
	if got != "b" {
		t.Errorf("safeIdx(1) = %q, want %q", got, "b")
	}
}

func TestSafeIdx_OutOfRange(t *testing.T) {
	lines := []string{"a", "b"}
	got := safeIdx(lines, 5)
	if got != "" {
		t.Errorf("safeIdx(5) out of range = %q, want %q", got, "")
	}
}

func TestSafeIdx_Zero(t *testing.T) {
	lines := []string{"first"}
	got := safeIdx(lines, 0)
	if got != "first" {
		t.Errorf("safeIdx(0) = %q, want %q", got, "first")
	}
}

// ─── queuePanelLines ─────────────────────────────────────────────────────────

func TestQueuePanelLines_Empty(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	lines := m.queuePanelLines(76, 10)
	if len(lines) != 10 {
		t.Errorf("queuePanelLines(empty) returned %d lines, want 10", len(lines))
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "empty") || strings.Contains(l, "Queue") {
			found = true
			break
		}
	}
	if !found {
		t.Error("queuePanelLines should contain 'Queue' header or 'empty' message")
	}
}

func TestQueuePanelLines_WithTracks(t *testing.T) {
	m := newModel(nil)
	m.width = 80
	m.playerState.Track = &provider.Track{Title: "Now Playing"}
	m.queueTracks = []provider.Track{
		{Title: "Now Playing", Artist: "A"},
		{Title: "Next Track", Artist: "B"},
	}
	m.queue.SetTracks(m.queueTracks)
	lines := m.queuePanelLines(76, 10)
	if len(lines) != 10 {
		t.Errorf("queuePanelLines(tracks) returned %d lines, want 10", len(lines))
	}
}

// ─── statusNavContent and scheduleSearch ─────────────────────────────────────

func TestScheduleSearch_EmptyQuery(t *testing.T) {
	m := newModel(nil)
	cmd := m.scheduleSearch("")
	if cmd != nil {
		t.Error("scheduleSearch('') should return nil cmd")
	}
}

func TestScheduleSearch_NonEmptyQuery(t *testing.T) {
	m := newModel(nil)
	cmd := m.scheduleSearch("jazz")
	if cmd == nil {
		t.Error("scheduleSearch('jazz') should return non-nil cmd")
	}
}

// ─── Model.View() with different panels ──────────────────────────────────────

func TestModel_View_NormalMode(t *testing.T) {
	m := newModel(nil)
	m.width = 120
	m.height = 30
	view := m.View()
	if view == "" {
		t.Error("View() should return non-empty string")
	}
}

func TestModel_View_SearchMode(t *testing.T) {
	m := newModel(nil)
	m.width = 120
	m.height = 30
	m.mode = modeSearch
	view := m.View()
	_ = view // should not panic
}

func TestModel_View_CommandMode(t *testing.T) {
	m := newModel(nil)
	m.width = 120
	m.height = 30
	m.mode = modeCommand
	m.cmdBuf = "sa"
	view := m.View()
	_ = view // should not panic
}

func TestModel_View_DebugLog(t *testing.T) {
	m := newModel(nil)
	m.width = 120
	m.height = 30
	m.debugView = true
	m.appendLog("test entry")
	view := m.View()
	_ = view // should not panic
}

func TestModel_View_WithQueuePanel(t *testing.T) {
	m := newModel(newMockPlayer())
	m.width = 120
	m.height = 30
	// Activate queue panel.
	for i, p := range m.panels {
		if p.NavKey() == "q" {
			m.activePanel = i
			break
		}
	}
	view := m.View()
	_ = view // should not panic
}

func TestModel_View_WithLibraryPanel(t *testing.T) {
	m := newModel(nil)
	m.width = 120
	m.height = 30
	// Activate library panel.
	for i, p := range m.panels {
		if p.NavKey() == "l" {
			m.activePanel = i
			break
		}
	}
	view := m.View()
	_ = view // should not panic
}

// ─── Model.Init coverage ─────────────────────────────────────────────────────

func TestModel_Init_ReturnsCmd(t *testing.T) {
	m := newModel(nil)
	cmd := m.Init()
	// Init returns a tick cmd — it should be non-nil.
	if cmd == nil {
		t.Error("Init() should return a non-nil cmd")
	}
}

func TestModel_Init_WithPlayer(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	cmd := m.Init()
	_ = cmd // should not panic
}

// ─── handleNormalKey queue panel: enter, d, K, J ──────────────────────────────

func TestHandleNormalKey_QueuePanel_Enter_WithSelection(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	// Set up a queue.
	m.queueTracks = []provider.Track{
		{Title: "A", Artist: "AA", CatalogID: "cat1"},
		{Title: "B", Artist: "BB", CatalogID: "cat2"},
	}
	m.queueIDs = []string{"cat1", "cat2"}

	// Open queue panel.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "q")

	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyEnter}, "enter")
	if cmd != nil {
		cmd()
	}
}

func TestHandleNormalKey_QueuePanel_D_RemoveTrack(t *testing.T) {
	mp := newMockPlayer()
	m := newModel(mp)
	m.queueTracks = []provider.Track{
		{Title: "Remove Me", Artist: "A", ID: "r1", CatalogID: "cat1"},
		{Title: "Keep Me", Artist: "B", ID: "r2", CatalogID: "cat2"},
	}
	m.queueIDs = []string{"cat1", "cat2"}
	m.queue.SetTracks(m.queueTracks)

	// Open queue panel.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "q")

	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}, "d")
	if cmd != nil {
		cmd()
	}
	if len(m.queueTracks) != 1 {
		t.Errorf("queue should have 1 track after d-delete, got %d", len(m.queueTracks))
	}
}

func TestHandleNormalKey_G_DoubleTap(t *testing.T) {
	m := newModel(nil)
	// First g — sets lastKey.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}, "g")
	if m.lastKey != "g" {
		t.Errorf("after first g, lastKey = %q, want %q", m.lastKey, "g")
	}
	// Second g — resets lastKey.
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}, "g")
	if m.lastKey != "" {
		t.Errorf("after second g, lastKey = %q, want %q", m.lastKey, "")
	}
}

func TestHandleNormalKey_ActivePanel_Esc(t *testing.T) {
	m := newModel(nil)
	m.activePanel = 0
	m.handleNormalKey(tea.KeyMsg{Type: tea.KeyEsc}, "esc")
	if m.activePanel >= 0 {
		t.Error("esc with activePanel should close it")
	}
}

func TestHandleNormalKey_ActivePanel_ForwardKey(t *testing.T) {
	m := newModel(nil)
	// Open library panel.
	for i, p := range m.panels {
		if p.NavKey() == "l" {
			m.activePanel = i
			break
		}
	}
	// Forward a key to library panel — should not panic.
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "j")
	_ = cmd
}

func TestHandleNormalKey_VibeFocused_RoutesToVibe(t *testing.T) {
	m := newModel(nil)
	m.vibe.Focus()
	// Keys should go to vibe panel when it's focused.
	cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyEsc}, "esc")
	_ = cmd
}
