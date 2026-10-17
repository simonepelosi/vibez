package player

import (
	"fmt"
	"sync"
)

const (
	// subChanSize is the handoff buffer between a subscriber's delivery
	// goroutine and its reader.
	subChanSize = 8

	// maxPendingQueue bounds the deliveries queued for a subscriber that has
	// stopped reading. Only log-carrying entries accumulate; plain state
	// updates coalesce into one.
	maxPendingQueue = 500
)

// Broadcast fans player state out to subscribers.
//
// State is coalescing: a subscriber that falls behind gets the newest update
// rather than every one it missed, which is the right trade for a position
// tick. Debug log entries are not coalescing. Each is queued in order and
// delivered even if the subscriber is slow, because the moment the log matters
// most, a burst around a failed play, is exactly when a shared buffer
// overflows.
//
// Each subscriber owns a goroutine that blocks on delivery so that no producer
// ever does. Subscriptions last until Close, which closes every channel handed
// out. Values already buffered in a channel stay readable after that, but
// entries still pending behind them are dropped: shutdown does not wait on a
// subscriber that may never read again.
type Broadcast struct {
	mu     sync.Mutex
	subs   []*subscriber
	closed bool
}

type subscriber struct {
	ch   chan State
	wake chan struct{} // capacity 1; a buffered signal is a pending pass
	done chan struct{} // closed by Broadcast.Close

	mu      sync.Mutex
	queue   []State
	dropped int // log entries discarded at the cap, reported on the next delivery
}

// Subscribe returns a channel receiving state updates and debug log entries.
// After Close it returns a channel that is already closed.
func (b *Broadcast) Subscribe() <-chan State {
	sub := &subscriber{
		ch:   make(chan State, subChanSize),
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(sub.ch)
		return sub.ch
	}
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	go sub.deliver()
	return sub.ch
}

// Send delivers s to every subscriber. It does nothing after Close.
func (b *Broadcast) Send(s State) {
	b.mu.Lock()
	subs := b.subs
	b.mu.Unlock()
	for _, sub := range subs {
		sub.push(s)
	}
}

// Close closes every subscriber channel and makes later sends no-ops. It is
// safe to call more than once and concurrently with Send.
func (b *Broadcast) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()
	for _, sub := range subs {
		close(sub.done)
	}
}

// SendLog delivers s to every subscriber carrying msg as a debug log entry.
// The entry is not dropped if the subscriber is behind; it waits its turn.
func (b *Broadcast) SendLog(s State, msg string) {
	s.Logs = []string{msg}
	b.Send(s)
}

// push queues s for delivery. It never blocks: a subscriber that has stopped
// reading must not stall the JS binding that produced the update.
func (sub *subscriber) push(s State) {
	select {
	case <-sub.done:
		return // a Send that raced Close
	default:
	}
	sub.mu.Lock()
	// Replace the newest queued entry when both it and s are state-only. A
	// subscriber that is behind wants the current position, not every tick it
	// missed, and this keeps an idle backlog from growing at all.
	if n := len(sub.queue); len(s.Logs) == 0 && n > 0 && len(sub.queue[n-1].Logs) == 0 {
		sub.queue[n-1] = s
	} else {
		sub.queue = append(sub.queue, s)
		sub.trim()
	}
	sub.mu.Unlock()

	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

// trim enforces maxPendingQueue by discarding the oldest entries, counting the
// log lines lost so the next delivery can say so rather than hide it.
func (sub *subscriber) trim() {
	excess := len(sub.queue) - maxPendingQueue
	if excess <= 0 {
		return
	}
	for _, s := range sub.queue[:excess] {
		sub.dropped += len(s.Logs)
	}
	sub.queue = append(sub.queue[:0], sub.queue[excess:]...)
}

// deliver drains the queue onto the subscriber's channel, blocking on the send
// so the producer does not have to. Close ends it even mid-send, and it is the
// only sender, so closing the channel here cannot race a delivery.
func (sub *subscriber) deliver() {
	defer func() {
		sub.mu.Lock()
		sub.queue = nil
		sub.mu.Unlock()
		close(sub.ch)
	}()
	for {
		select {
		case <-sub.wake:
		case <-sub.done:
			return
		}
		for {
			sub.mu.Lock()
			if len(sub.queue) == 0 {
				sub.mu.Unlock()
				break
			}
			s := sub.queue[0]
			sub.queue[0] = State{} // release the popped entry's track pointer
			sub.queue = sub.queue[1:]
			if sub.dropped > 0 {
				note := fmt.Sprintf("[log] %d earlier entries dropped", sub.dropped)
				s.Logs = append([]string{note}, s.Logs...)
				sub.dropped = 0
			}
			sub.mu.Unlock()

			// Checked first so a closed broadcast stops at once rather than
			// whenever select happens to pick done over free buffer space.
			select {
			case <-sub.done:
				return
			default:
			}
			select {
			case sub.ch <- s:
			case <-sub.done:
				return
			}
		}
	}
}
