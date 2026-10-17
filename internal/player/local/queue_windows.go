//go:build windows

package local

import (
	"math/rand/v2"
	"slices"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// queue is the play order of a local session. The TUI keeps its own copy of
// the queue and addresses entries by index, so every mutation here follows the
// same arithmetic as the TUI's slice: MoveInQueue(from, to) leaves the track at
// index to, and removing an entry keeps the others in order.
//
// cur is the entry playback is on, or the one Play would start while nothing
// is loaded; it is -1 only when the queue is empty.
//
// Shuffle never reorders entries, as that would break the TUI's indices.
// It keeps a play order of entry keys instead: the entries up to the current
// one as history, then the rest at random. Keys stay valid through moves and
// removals, which only indices do not.
type queue struct {
	entries []queueEntry
	cur     int
	lastKey uint64
	repeat  int
	shuffle bool
	order   []uint64 // shuffle play order of entry keys; nil unless shuffle
}

type queueEntry struct {
	key   uint64
	track provider.Track
}

func newQueue() queue { return queue{cur: -1} }

func (q *queue) current() (provider.Track, bool) {
	if q.cur < 0 {
		return provider.Track{}, false
	}
	return q.entries[q.cur].track, true
}

// reset replaces the queue with tracks, the first of them current.
func (q *queue) reset(tracks []provider.Track) {
	q.entries = make([]queueEntry, 0, len(tracks))
	q.push(tracks)
	q.cur = min(0, len(q.entries)-1)
	q.order = nil
	if q.shuffle {
		q.order = q.shuffledFromCurrent()
	}
}

// add appends tracks. With start set, or when the queue was empty, the first
// of them becomes current, and under shuffle it also plays before the rest of
// the additions, which land at random among the entries still to play.
func (q *queue) add(tracks []provider.Track, start bool) {
	if len(tracks) == 0 {
		return
	}
	start = start || q.cur < 0
	first := len(q.entries)
	q.push(tracks)
	if q.shuffle {
		at := q.orderPos() + 1
		added := q.entries[first:]
		if start {
			q.order = slices.Insert(q.order, at, added[0].key)
			added = added[1:]
			at++
		}
		for _, entry := range added {
			q.order = slices.Insert(q.order, at+rand.IntN(len(q.order)-at+1), entry.key) //nolint:gosec // music shuffle
		}
	}
	if start {
		q.cur = first
	}
}

// remove deletes the entry at idx and reports whether it was the current one
// and, if so, whether another entry took over: the one that slid into its
// place, or under shuffle the next in play order. When none did, cur rests on
// the last entry in play order, or -1 once the queue is empty. ok is false
// when idx is out of range.
func (q *queue) remove(idx int) (wasCurrent, succeeded, ok bool) {
	if idx < 0 || idx >= len(q.entries) {
		return false, false, false
	}
	wasCurrent = idx == q.cur
	var successor uint64
	if q.shuffle {
		pos := slices.Index(q.order, q.entries[idx].key)
		q.order = slices.Delete(q.order, pos, pos+1)
		switch {
		case !wasCurrent:
		case pos < len(q.order):
			successor, succeeded = q.order[pos], true
		case len(q.order) > 0:
			successor = q.order[len(q.order)-1]
		}
	}
	q.entries = slices.Delete(q.entries, idx, idx+1)
	switch {
	case len(q.entries) == 0:
		q.cur = -1
		succeeded = false
	case idx < q.cur:
		q.cur--
	case !wasCurrent:
	case q.shuffle:
		q.cur = q.indexOf(successor)
	case idx < len(q.entries):
		succeeded = true
	default:
		q.cur = len(q.entries) - 1
	}
	return wasCurrent, succeeded, true
}

// move moves the entry at from so that it ends up at index to.
func (q *queue) move(from, to int) bool {
	n := len(q.entries)
	if from < 0 || from >= n || to < 0 || to >= n {
		return false
	}
	e := q.entries[from]
	if from < to {
		copy(q.entries[from:to], q.entries[from+1:to+1])
	} else {
		copy(q.entries[to+1:from+1], q.entries[to:from])
	}
	q.entries[to] = e
	switch {
	case q.cur == from:
		q.cur = to
	case from < q.cur && to >= q.cur:
		q.cur--
	case from > q.cur && to <= q.cur:
		q.cur++
	}
	return true
}

func (q *queue) clear() {
	q.entries = nil
	q.cur = -1
	q.order = nil
}

// advance moves cur to the entry that plays next and reports whether there
// is one. auto marks a track that ended by itself, the only case repeat-one
// replays; a skip always moves on.
func (q *queue) advance(auto bool) bool {
	if q.cur < 0 {
		return false
	}
	if auto && q.repeat == player.RepeatModeOne {
		return true
	}
	if !q.shuffle {
		switch {
		case q.cur+1 < len(q.entries):
			q.cur++
		case q.repeat == player.RepeatModeAll:
			q.cur = 0
		default:
			return false
		}
		return true
	}
	pos := q.orderPos()
	switch {
	case pos+1 < len(q.order):
		q.cur = q.indexOf(q.order[pos+1])
	case q.repeat == player.RepeatModeAll:
		q.newShuffleCycle()
		q.cur = q.indexOf(q.order[0])
	default:
		return false
	}
	return true
}

// previous moves cur to the entry that played before, wrapping only under
// repeat-all; at the start otherwise cur stays and the track restarts.
func (q *queue) previous() {
	if q.cur < 0 {
		return
	}
	if !q.shuffle {
		switch {
		case q.cur > 0:
			q.cur--
		case q.repeat == player.RepeatModeAll:
			q.cur = len(q.entries) - 1
		}
		return
	}
	pos := q.orderPos()
	switch {
	case pos > 0:
		q.cur = q.indexOf(q.order[pos-1])
	case q.repeat == player.RepeatModeAll:
		q.cur = q.indexOf(q.order[len(q.order)-1])
	}
}

func (q *queue) setShuffle(on bool) {
	q.shuffle = on
	q.order = nil
	if on {
		q.order = q.shuffledFromCurrent()
	}
}

func (q *queue) push(tracks []provider.Track) {
	for _, t := range tracks {
		q.lastKey++
		q.entries = append(q.entries, queueEntry{key: q.lastKey, track: t})
	}
}

// shuffledFromCurrent is the play order for turning shuffle on: the entries
// up to and including the current one in queue order, then the rest shuffled.
func (q *queue) shuffledFromCurrent() []uint64 {
	order := make([]uint64, len(q.entries))
	for i, e := range q.entries {
		order[i] = e.key
	}
	rest := order[q.cur+1:]
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] }) //nolint:gosec // music shuffle
	return order
}

// newShuffleCycle reshuffles every entry for another pass under repeat-all,
// keeping the track that just played from coming straight back.
func (q *queue) newShuffleCycle() {
	cur := q.entries[q.cur].key
	rand.Shuffle(len(q.order), func(i, j int) { q.order[i], q.order[j] = q.order[j], q.order[i] }) //nolint:gosec // music shuffle
	if len(q.order) > 1 && q.order[0] == cur {
		j := 1 + rand.IntN(len(q.order)-1) //nolint:gosec // music shuffle
		q.order[0], q.order[j] = q.order[j], q.order[0]
	}
}

func (q *queue) orderPos() int {
	if q.cur < 0 {
		return -1
	}
	return slices.Index(q.order, q.entries[q.cur].key)
}

func (q *queue) indexOf(key uint64) int {
	return slices.IndexFunc(q.entries, func(e queueEntry) bool { return e.key == key })
}
