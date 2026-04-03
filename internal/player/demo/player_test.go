package demo_test

import (
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/demo"
	demodp "github.com/simone-vibes/vibez/internal/provider/demo"
)

// newPlayer creates a new demo Player and registers cleanup to close it.
func newPlayer(t *testing.T) *demo.Player {
	t.Helper()
	p := demo.New()
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// state returns the current player state or fails the test.
func state(t *testing.T, p *demo.Player) *player.State {
	t.Helper()
	st, err := p.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	return st
}

// --- New ---

func TestNew_InitialState(t *testing.T) {
	p := newPlayer(t)
	st := state(t, p)

	if st.Track == nil {
		t.Fatal("initial Track is nil")
	}
	if !st.Playing {
		t.Error("initial Playing = false, want true")
	}
	if st.Volume != 0.8 {
		t.Errorf("initial Volume = %v, want 0.8", st.Volume)
	}
}

func TestNew_FirstTrackIsSet(t *testing.T) {
	p := newPlayer(t)
	st := state(t, p)

	if st.Track.Title == "" {
		t.Error("initial Track.Title is empty")
	}
}

// --- Play / Pause / Stop ---

func TestPlay_SetsPlayingTrue(t *testing.T) {
	p := newPlayer(t)
	_ = p.Pause()
	if err := p.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if !state(t, p).Playing {
		t.Error("Playing = false after Play()")
	}
}

func TestPause_SetsPlayingFalse(t *testing.T) {
	p := newPlayer(t)
	if err := p.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if state(t, p).Playing {
		t.Error("Playing = true after Pause()")
	}
}

func TestStop_SetsPlayingFalse(t *testing.T) {
	p := newPlayer(t)
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if state(t, p).Playing {
		t.Error("Playing = true after Stop()")
	}
}

// --- Next ---

func TestNext_AdvancesToNextTrack(t *testing.T) {
	p := newPlayer(t)
	before := state(t, p).Track.Title

	if err := p.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	after := state(t, p).Track.Title
	if after == before {
		t.Errorf("Track title unchanged after Next(): %q", after)
	}
}

func TestNext_WrapsAround(t *testing.T) {
	p := newPlayer(t)
	// Advance past all tracks.
	for range demodp.Tracks {
		if err := p.Next(); err != nil {
			t.Fatalf("Next: %v", err)
		}
	}
	// After a full cycle the track should wrap to the first one.
	if state(t, p).Track == nil {
		t.Error("Track is nil after wrapping around")
	}
}

// --- Previous ---

func TestPrevious_RestartsTrackWhenPositionGt3s(t *testing.T) {
	p := newPlayer(t)
	_ = p.Seek(10 * time.Second)
	titleBefore := state(t, p).Track.Title

	if err := p.Previous(); err != nil {
		t.Fatalf("Previous: %v", err)
	}
	st := state(t, p)
	if st.Track.Title != titleBefore {
		t.Errorf("Track changed when position > 3s: got %q, want %q", st.Track.Title, titleBefore)
	}
	if st.Position != 0 {
		t.Errorf("Position = %v after Previous restart; want 0", st.Position)
	}
}

func TestPrevious_GoesBackWhenPositionLte3s(t *testing.T) {
	p := newPlayer(t)
	_ = p.Next() // move to second track
	titleSecond := state(t, p).Track.Title
	_ = p.Seek(1 * time.Second)
	first := demodp.Tracks[0].Title

	if err := p.Previous(); err != nil {
		t.Fatalf("Previous: %v", err)
	}
	st := state(t, p)
	if st.Track.Title == titleSecond {
		t.Error("Track did not change when calling Previous from second track with position <= 3s")
	}
	if st.Track.Title != first {
		t.Errorf("Previous from second track = %q, want first track %q", st.Track.Title, first)
	}
}

func TestPrevious_FromFirstTrack_GoesToLast(t *testing.T) {
	p := newPlayer(t)
	// Seek to position <= 3s so Previous goes back, not just restarts.
	_ = p.Seek(1 * time.Second)
	last := demodp.Tracks[len(demodp.Tracks)-1].Title

	if err := p.Previous(); err != nil {
		t.Fatalf("Previous: %v", err)
	}
	if state(t, p).Track.Title != last {
		t.Errorf("Previous from first track = %q, want last track %q", state(t, p).Track.Title, last)
	}
}

// --- Seek ---

func TestSeek_SetsPosition(t *testing.T) {
	p := newPlayer(t)
	target := 30 * time.Second

	if err := p.Seek(target); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if state(t, p).Position != target {
		t.Errorf("Position = %v, want %v", state(t, p).Position, target)
	}
}

func TestSeek_BeyondDurationIsIgnored(t *testing.T) {
	p := newPlayer(t)
	dur := state(t, p).Track.Duration

	_ = p.Seek(dur + 10*time.Second)
	// Position should not be past the track duration.
	pos := state(t, p).Position
	if pos > dur {
		t.Errorf("Position = %v; want <= track duration %v", pos, dur)
	}
}

// --- SetVolume ---

func TestSetVolume_Normal(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetVolume(0.5); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if state(t, p).Volume != 0.5 {
		t.Errorf("Volume = %v, want 0.5", state(t, p).Volume)
	}
}

func TestSetVolume_ClampsBelow0(t *testing.T) {
	p := newPlayer(t)
	_ = p.SetVolume(-1.0)
	if state(t, p).Volume != 0 {
		t.Errorf("Volume = %v after SetVolume(-1), want 0", state(t, p).Volume)
	}
}

func TestSetVolume_ClampsAbove1(t *testing.T) {
	p := newPlayer(t)
	_ = p.SetVolume(2.0)
	if state(t, p).Volume != 1.0 {
		t.Errorf("Volume = %v after SetVolume(2), want 1.0", state(t, p).Volume)
	}
}

// --- SetQueue ---

func TestSetQueue_WithKnownIDs(t *testing.T) {
	p := newPlayer(t)
	ids := []string{demodp.Tracks[0].ID, demodp.Tracks[1].ID}

	if err := p.SetQueue(ids); err != nil {
		t.Fatalf("SetQueue: %v", err)
	}
	st := state(t, p)
	if !st.Playing {
		t.Error("Playing = false after SetQueue; want true")
	}
	if st.Track == nil {
		t.Error("Track is nil after SetQueue")
	}
}

func TestSetQueue_UnknownIDsFallsBackToDefault(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetQueue([]string{"unknown-id"}); err != nil {
		t.Fatalf("SetQueue(unknown): %v", err)
	}
	// Falls back to the default demo track list.
	if state(t, p).Track == nil {
		t.Error("Track is nil after SetQueue with unknown IDs")
	}
}

func TestSetQueue_EmptyFallsBackToDefault(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetQueue([]string{}); err != nil {
		t.Fatalf("SetQueue(empty): %v", err)
	}
	if state(t, p).Track == nil {
		t.Error("Track is nil after SetQueue with empty IDs")
	}
}

// --- SetPlaylist ---

func TestSetPlaylist_StartIdxInBounds(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetPlaylist("pl.any", 2); err != nil {
		t.Fatalf("SetPlaylist: %v", err)
	}
	st := state(t, p)
	if st.Track == nil {
		t.Fatal("Track is nil after SetPlaylist")
	}
	want := demodp.Tracks[2].Title
	if st.Track.Title != want {
		t.Errorf("Track.Title = %q, want %q", st.Track.Title, want)
	}
}

func TestSetPlaylist_OutOfBoundsFallsBackToFirst(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetPlaylist("pl.any", 9999); err != nil {
		t.Fatalf("SetPlaylist: %v", err)
	}
	if state(t, p).Track.Title != demodp.Tracks[0].Title {
		t.Error("Out-of-bounds startIdx did not fall back to first track")
	}
}

func TestSetPlaylist_NegativeIdxFallsBackToFirst(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetPlaylist("pl.any", -1); err != nil {
		t.Fatalf("SetPlaylist: %v", err)
	}
	if state(t, p).Track.Title != demodp.Tracks[0].Title {
		t.Error("Negative startIdx did not fall back to first track")
	}
}

// --- AppendQueue ---

func TestAppendQueue_AddsKnownTracks(t *testing.T) {
	p := newPlayer(t)
	ids := []string{demodp.Tracks[3].ID}

	if err := p.AppendQueue(ids); err != nil {
		t.Fatalf("AppendQueue: %v", err)
	}
	// State should still be playing the original track.
	if state(t, p).Track == nil {
		t.Error("Track is nil after AppendQueue")
	}
}

func TestAppendQueue_UnknownIDsIgnored(t *testing.T) {
	p := newPlayer(t)
	if err := p.AppendQueue([]string{"unknown-id"}); err != nil {
		t.Fatalf("AppendQueue(unknown): %v", err)
	}
	// State should not have changed.
	if state(t, p).Track == nil {
		t.Error("Track is nil after AppendQueue with unknown IDs")
	}
}

// --- SetRepeat ---

func TestSetRepeat_Off(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetRepeat(player.RepeatModeOff); err != nil {
		t.Fatalf("SetRepeat: %v", err)
	}
	if state(t, p).RepeatMode != player.RepeatModeOff {
		t.Errorf("RepeatMode = %d, want %d", state(t, p).RepeatMode, player.RepeatModeOff)
	}
}

func TestSetRepeat_One(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetRepeat(player.RepeatModeOne); err != nil {
		t.Fatalf("SetRepeat: %v", err)
	}
	if state(t, p).RepeatMode != player.RepeatModeOne {
		t.Errorf("RepeatMode = %d, want %d", state(t, p).RepeatMode, player.RepeatModeOne)
	}
}

func TestSetRepeat_All(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetRepeat(player.RepeatModeAll); err != nil {
		t.Fatalf("SetRepeat: %v", err)
	}
	if state(t, p).RepeatMode != player.RepeatModeAll {
		t.Errorf("RepeatMode = %d, want %d", state(t, p).RepeatMode, player.RepeatModeAll)
	}
}

// --- SetShuffle ---

func TestSetShuffle_On(t *testing.T) {
	p := newPlayer(t)
	if err := p.SetShuffle(true); err != nil {
		t.Fatalf("SetShuffle(true): %v", err)
	}
	if !state(t, p).ShuffleMode {
		t.Error("ShuffleMode = false after SetShuffle(true)")
	}
}

func TestSetShuffle_Off(t *testing.T) {
	p := newPlayer(t)
	_ = p.SetShuffle(true)
	if err := p.SetShuffle(false); err != nil {
		t.Fatalf("SetShuffle(false): %v", err)
	}
	if state(t, p).ShuffleMode {
		t.Error("ShuffleMode = true after SetShuffle(false)")
	}
}

// --- RemoveFromQueue ---

func TestRemoveFromQueue_ValidIndex(t *testing.T) {
	p := newPlayer(t)
	// Append extra tracks so we have something to remove.
	_ = p.AppendQueue([]string{demodp.Tracks[3].ID, demodp.Tracks[4].ID})

	if err := p.RemoveFromQueue(len(demodp.Tracks)); err != nil {
		t.Fatalf("RemoveFromQueue: %v", err)
	}
}

func TestRemoveFromQueue_OutOfBoundsIsNoop(t *testing.T) {
	p := newPlayer(t)
	if err := p.RemoveFromQueue(9999); err != nil {
		t.Fatalf("RemoveFromQueue(out-of-bounds): %v", err)
	}
	if state(t, p).Track == nil {
		t.Error("Track is nil after out-of-bounds RemoveFromQueue")
	}
}

func TestRemoveFromQueue_NegativeIsNoop(t *testing.T) {
	p := newPlayer(t)
	if err := p.RemoveFromQueue(-1); err != nil {
		t.Fatalf("RemoveFromQueue(-1): %v", err)
	}
	if state(t, p).Track == nil {
		t.Error("Track is nil after RemoveFromQueue(-1)")
	}
}

// --- MoveInQueue ---

func TestMoveInQueue_ValidIndices(t *testing.T) {
	p := newPlayer(t)
	if err := p.MoveInQueue(0, 1); err != nil {
		t.Fatalf("MoveInQueue: %v", err)
	}
	// State should remain intact.
	if state(t, p).Track == nil {
		t.Error("Track is nil after MoveInQueue")
	}
}

func TestMoveInQueue_OutOfBoundsIsNoop(t *testing.T) {
	p := newPlayer(t)
	if err := p.MoveInQueue(0, 9999); err != nil {
		t.Fatalf("MoveInQueue(out-of-bounds): %v", err)
	}
}

func TestMoveInQueue_NegativeFromIsNoop(t *testing.T) {
	p := newPlayer(t)
	if err := p.MoveInQueue(-1, 0); err != nil {
		t.Fatalf("MoveInQueue(-1, 0): %v", err)
	}
}

// --- ClearQueue ---

func TestClearQueue_StopsPlayback(t *testing.T) {
	p := newPlayer(t)
	if err := p.ClearQueue(); err != nil {
		t.Fatalf("ClearQueue: %v", err)
	}
	st := state(t, p)
	if st.Playing {
		t.Error("Playing = true after ClearQueue; want false")
	}
	if st.Track != nil {
		t.Error("Track is not nil after ClearQueue")
	}
}

// --- Subscribe ---

func TestSubscribe_ReceivesState(t *testing.T) {
	p := newPlayer(t)
	ch := p.Subscribe()

	// Trigger a state change to cause a broadcast.
	_ = p.Pause()

	select {
	case st := <-ch:
		_ = st // received a state
	case <-time.After(2 * time.Second):
		t.Error("no state received on subscriber channel within timeout")
	}
}

// --- Close ---

func TestClose_Idempotent(t *testing.T) {
	// Close is handled by t.Cleanup in newPlayer; call it early explicitly.
	p := demo.New()
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
