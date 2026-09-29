//go:build windows

package local

import (
	"fmt"
	"slices"
	"testing"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

func queueOf(ids ...string) queue {
	q := newQueue()
	tracks := make([]provider.Track, len(ids))
	for i, id := range ids {
		tracks[i] = provider.Track{ID: id}
	}
	q.reset(tracks)
	return q
}

func numberedIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%02d", i)
	}
	return ids
}

func (q *queue) ids() []string {
	out := make([]string, len(q.entries))
	for i, e := range q.entries {
		out[i] = e.track.ID
	}
	return out
}

func (q *queue) currentID() string {
	t, ok := q.current()
	if !ok {
		return ""
	}
	return t.ID
}

// playOut advances until the queue runs out, returning the IDs played after
// the current one. It fails rather than loop forever.
func playOut(t *testing.T, q *queue) []string {
	t.Helper()
	var played []string
	for q.advance(true) {
		played = append(played, q.currentID())
		if len(played) > 10*len(q.entries)+10 {
			t.Fatalf("queue never ran out; played %v", played)
		}
	}
	return played
}

// The TUI reorders its own copy of the queue and sends the same indices, so
// the two must agree after every move, including the append-then-move the
// TUI uses to insert tracks after the current one.
func TestQueueMovesMatchTheTUIsCopy(t *testing.T) {
	q := queueOf("a", "b", "c", "d", "e")
	q.cur = 1 // playing b
	tui := []string{"a", "b", "c", "d", "e"}

	swap := func(i, j int) { tui[i], tui[j] = tui[j], tui[i] }
	swap(3, 2) // K on d: MoveInQueue(3, 2)
	q.move(3, 2)
	swap(0, 1) // J on a: MoveInQueue(0, 1)
	q.move(0, 1)
	// Play next x, y with b current at 0: the TUI appends, then moves each
	// from the end to just after b.
	tui = slices.Insert(tui, 1, "x", "y")
	q.add([]provider.Track{{ID: "x"}, {ID: "y"}}, false)
	q.move(5, 1)
	q.move(6, 2)

	if got := q.ids(); !slices.Equal(got, tui) {
		t.Fatalf("queue = %v, TUI copy = %v", got, tui)
	}
	if got := q.currentID(); got != "b" {
		t.Fatalf("current = %q after moves, want b", got)
	}
	if got := playOut(t, &q); !slices.Equal(got, []string{"x", "y", "a", "d", "c", "e"}) {
		t.Fatalf("played %v after b, want x y a d c e", got)
	}
	if q.move(0, 7) || q.move(-1, 0) {
		t.Fatal("move accepted an out-of-range index")
	}
}

func TestQueueRemove(t *testing.T) {
	q := queueOf("a", "b", "c", "d")
	q.cur = 2 // c

	if wasCurrent, _, _ := q.remove(0); wasCurrent || q.currentID() != "c" {
		t.Fatalf("removing an earlier entry: wasCurrent=%v current=%q, want false c", wasCurrent, q.currentID())
	}
	// [b c d], current c: removing it hands over to d, which slid into place.
	if wasCurrent, succeeded, _ := q.remove(1); !wasCurrent || !succeeded || q.currentID() != "d" {
		t.Fatalf("removing current: wasCurrent=%v succeeded=%v current=%q, want true true d", wasCurrent, succeeded, q.currentID())
	}
	// [b d], current d, the last: nothing takes over and b stays current.
	if wasCurrent, succeeded, _ := q.remove(1); !wasCurrent || succeeded || q.currentID() != "b" {
		t.Fatalf("removing current last: wasCurrent=%v succeeded=%v current=%q, want true false b", wasCurrent, succeeded, q.currentID())
	}
	if _, succeeded, _ := q.remove(0); succeeded || q.cur != -1 {
		t.Fatalf("removing the only entry: succeeded=%v cur=%d, want false -1", succeeded, q.cur)
	}
	if _, _, ok := q.remove(0); ok {
		t.Fatal("remove on an empty queue reported ok")
	}
}

func TestQueueRepeat(t *testing.T) {
	q := queueOf("a", "b")
	q.repeat = player.RepeatModeOne
	if !q.advance(true) || q.currentID() != "a" {
		t.Fatalf("repeat-one end of track: current %q, want a again", q.currentID())
	}
	if !q.advance(false) || q.currentID() != "b" {
		t.Fatalf("repeat-one skip: current %q, want b", q.currentID())
	}
	q.repeat = player.RepeatModeAll
	if !q.advance(true) || q.currentID() != "a" {
		t.Fatalf("repeat-all past the end: current %q, want a", q.currentID())
	}
	q.previous()
	if q.currentID() != "b" {
		t.Fatalf("repeat-all previous from the start: current %q, want b", q.currentID())
	}
	q.repeat = player.RepeatModeOff
	q.previous()
	q.previous()
	if q.currentID() != "a" {
		t.Fatalf("previous at the start with repeat off: current %q, want a", q.currentID())
	}
	q.cur = 1
	if q.advance(false) {
		t.Fatal("advance past the end with repeat off reported a next track")
	}
}

// Shuffle plays every entry once, starting from the current one, without
// reordering the entries the TUI addresses by index.
func TestQueueShufflePlaysEachEntryOnce(t *testing.T) {
	ids := numberedIDs(20)
	q := queueOf(ids...)
	q.advance(false)
	q.advance(false) // current t02, t00 and t01 already played
	q.setShuffle(true)
	if !slices.Equal(q.ids(), ids) {
		t.Fatal("turning shuffle on reordered the queue")
	}
	played := playOut(t, &q)
	slices.Sort(played)
	if !slices.Equal(played, ids[3:]) {
		t.Fatalf("shuffle played %v, want each of %v once", played, ids[3:])
	}
}

// Under shuffle, Previous retraces what played, and entries added or removed
// mid-pass are played or skipped without disturbing the rest of the pass.
func TestQueueShuffleFollowsQueueChanges(t *testing.T) {
	q := queueOf(numberedIDs(10)...)
	q.setShuffle(true)
	var seen []string
	seen = append(seen, q.currentID())
	for range 3 {
		q.advance(false)
		seen = append(seen, q.currentID())
	}
	// Previous retraces what played.
	for i := len(seen) - 2; i >= 0; i-- {
		q.previous()
		if q.currentID() != seen[i] {
			t.Fatalf("previous went to %q, want %q (history %v)", q.currentID(), seen[i], seen)
		}
	}
	for range 3 {
		q.advance(false)
	}

	q.add([]provider.Track{{ID: "new1"}, {ID: "new2"}}, false)
	var removed string
	for i, e := range q.entries {
		if i != q.cur && !slices.Contains(seen, e.track.ID) {
			removed = e.track.ID
			q.remove(i)
			break
		}
	}
	rest := playOut(t, &q)
	all := append(slices.Clone(seen), rest...)
	slices.Sort(all)
	want := slices.DeleteFunc(append(numberedIDs(10), "new1", "new2"), func(id string) bool { return id == removed })
	slices.Sort(want)
	if !slices.Equal(all, want) {
		t.Fatalf("played %v then %v; want each of %v exactly once", seen, rest, want)
	}
}

// Appending with start, as AppendQueue does once the queue has played out,
// jumps to the first addition and keeps the unplayed rest of the pass.
func TestQueueShuffleStartsAddedTracksFirst(t *testing.T) {
	q := queueOf(numberedIDs(6)...)
	q.setShuffle(true)
	q.add([]provider.Track{{ID: "x"}, {ID: "y"}}, true)
	if q.currentID() != "x" || q.cur != 6 {
		t.Fatalf("current = %q at %d, want x at 6", q.currentID(), q.cur)
	}
	played := playOut(t, &q)
	slices.Sort(played)
	want := numberedIDs(6)[1:]
	want = append(want, "y")
	if !slices.Equal(played, want) {
		t.Fatalf("after x played %v, want each of %v once", played, want)
	}
}

func TestQueueShuffleRepeatAllReshufflesEachPass(t *testing.T) {
	ids := numberedIDs(8)
	q := queueOf(ids...)
	q.repeat = player.RepeatModeAll
	q.setShuffle(true)
	pass := []string{q.currentID()}
	for range len(ids) - 1 {
		q.advance(true)
		pass = append(pass, q.currentID())
	}
	for range 5 {
		last := pass[len(pass)-1]
		pass = pass[:0]
		for range ids {
			if !q.advance(true) {
				t.Fatal("repeat-all shuffle ran out")
			}
			pass = append(pass, q.currentID())
		}
		if pass[0] == last {
			t.Fatalf("new pass started with %q, the track that just played", last)
		}
		sorted := slices.Sorted(slices.Values(pass))
		if !slices.Equal(sorted, ids) {
			t.Fatalf("a repeat-all pass played %v, want every entry once", pass)
		}
	}
}
