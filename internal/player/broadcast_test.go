package player

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// collectLogs reads log entries from ch until it has want of them or the read
// stalls, so a test failure reports what actually arrived.
func collectLogs(t *testing.T, ch <-chan State, want int) []string {
	t.Helper()
	var got []string
	for len(got) < want {
		select {
		case s := <-ch:
			got = append(got, s.Logs...)
		case <-time.After(2 * time.Second):
			return got
		}
	}
	return got
}

func TestBroadcastKeepsLogsPastAFullBuffer(t *testing.T) {
	var b Broadcast
	ch := b.Subscribe()

	const n = subChanSize * 4
	for i := range n {
		b.SendLog(State{}, fmt.Sprintf("line %d", i))
	}

	got := collectLogs(t, ch, n)
	if len(got) != n {
		t.Fatalf("got %d log entries, want %d", len(got), n)
	}
	for i, line := range got {
		if want := fmt.Sprintf("line %d", i); line != want {
			t.Fatalf("entry %d = %q, want %q", i, line, want)
		}
	}
}

// A log entry must arrive even when nothing follows it. The JS side polls state
// deduplicated, so an idle player pushes nothing that a backlog could ride out on.
func TestBroadcastDeliversLogsWithNoFurtherState(t *testing.T) {
	var b Broadcast
	ch := b.Subscribe()

	for i := range subChanSize * 3 {
		b.Send(State{Bitrate: i})
	}
	b.SendLog(State{Bitrate: 999}, "the last thing before it went quiet")

	got := collectLogs(t, ch, 1)
	if len(got) != 1 || got[0] != "the last thing before it went quiet" {
		t.Fatalf("got %v, want the final log entry", got)
	}
}

func TestBroadcastCoalescesStateOnlyUpdates(t *testing.T) {
	sub := &subscriber{ch: make(chan State, subChanSize), wake: make(chan struct{}, 1)}

	// No delivery goroutine, so everything stays queued.
	for i := range maxPendingQueue * 2 {
		sub.push(State{Bitrate: i})
	}

	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.queue) != 1 {
		t.Fatalf("queued %d state-only updates, want 1", len(sub.queue))
	}
	if got := sub.queue[0].Bitrate; got != maxPendingQueue*2-1 {
		t.Fatalf("kept update %d, want the newest (%d)", got, maxPendingQueue*2-1)
	}
}

func TestBroadcastBacklogIsPerSubscriber(t *testing.T) {
	var b Broadcast
	fast := b.Subscribe()
	slow := b.Subscribe()

	const n = subChanSize * 3
	done := make(chan []string, 1)
	go func() {
		done <- collectLogs(t, fast, n)
	}()

	for i := range n {
		b.SendLog(State{}, fmt.Sprintf("line %d", i))
	}

	if got := <-done; len(got) != n {
		t.Fatalf("fast subscriber got %d entries, want %d", len(got), n)
	}
	// The subscriber that read nothing while the other kept up must still have
	// everything waiting for it.
	if got := collectLogs(t, slow, n); len(got) != n {
		t.Fatalf("slow subscriber got %d entries, want %d", len(got), n)
	}
}

func TestBroadcastBacklogIsCappedAndReportsTheLoss(t *testing.T) {
	sub := &subscriber{ch: make(chan State, subChanSize), wake: make(chan struct{}, 1)}

	const overflow = 50
	for i := range maxPendingQueue + overflow {
		sub.push(State{Logs: []string{fmt.Sprintf("line %d", i)}})
	}

	sub.mu.Lock()
	queued, dropped := len(sub.queue), sub.dropped
	sub.mu.Unlock()
	if queued != maxPendingQueue {
		t.Fatalf("queue holds %d entries, cap is %d", queued, maxPendingQueue)
	}
	if dropped != overflow {
		t.Fatalf("counted %d dropped entries, want %d", dropped, overflow)
	}

	go sub.deliver()
	got := collectLogs(t, sub.ch, 1)
	if !strings.HasPrefix(got[0], "[log] ") || !strings.HasSuffix(got[0], " earlier entries dropped") {
		t.Fatalf("first entry after an overflow = %q, want a dropped-entry note", got[0])
	}
}

func TestBroadcastNeverBlocksOnAnUnreadSubscriber(t *testing.T) {
	var b Broadcast
	_ = b.Subscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range maxPendingQueue * 2 {
			b.SendLog(State{}, fmt.Sprintf("line %d", i))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked on a subscriber that is not reading")
	}
}

// drainUntilClosed reads ch until it is closed, failing if that takes longer
// than a stalled delivery goroutine could explain, and returns what it read.
func drainUntilClosed(t *testing.T, ch <-chan State) []State {
	t.Helper()
	var got []State
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, s)
		case <-deadline:
			t.Fatalf("channel still open after Close; read %d values", len(got))
		}
	}
}

// A range loop over a subscription, such as the Discord presence loop, has to
// end when the player shuts down, and it must still see what was already
// handed to it.
func TestBroadcastCloseEndsSubscriptionsKeepingBufferedValues(t *testing.T) {
	var b Broadcast
	ch := b.Subscribe()
	b.SendLog(State{}, "before close")
	// Wait for the entry to reach the channel buffer; Close only promises to
	// keep values that were already there.
	deadline := time.Now().Add(2 * time.Second)
	for len(ch) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	b.Close()
	got := drainUntilClosed(t, ch)
	if len(got) != 1 || len(got[0].Logs) != 1 || got[0].Logs[0] != "before close" {
		t.Fatalf("got %+v, want the one entry buffered before Close", got)
	}
}

// A subscriber that stopped reading, like the TUI after it exits, leaves its
// delivery goroutine blocked on a full channel with a backlog behind it. Close
// must free that goroutine without first pushing the whole backlog through.
func TestBroadcastCloseDropsBacklogOfUnreadSubscriber(t *testing.T) {
	var b Broadcast
	ch := b.Subscribe()
	for i := range maxPendingQueue {
		b.SendLog(State{}, fmt.Sprintf("line %d", i))
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(ch) < subChanSize && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	b.Close()
	got := drainUntilClosed(t, ch)
	// The buffer, plus at most the one entry deliver held when Close landed.
	if len(got) > subChanSize+1 {
		t.Fatalf("read %d entries after Close, want at most %d: the backlog was delivered instead of dropped", len(got), subChanSize+1)
	}
}

func TestBroadcastAfterClose(t *testing.T) {
	var b Broadcast
	b.Close()
	b.Close()
	b.Send(State{Bitrate: 1})
	b.SendLog(State{}, "ignored")
	select {
	case _, ok := <-b.Subscribe():
		if ok {
			t.Fatal("Subscribe after Close delivered a value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe after Close returned an open channel")
	}
}

// Close racing live sends must neither panic nor leave a subscriber open.
func TestBroadcastCloseRacingSends(t *testing.T) {
	var b Broadcast
	chans := []<-chan State{b.Subscribe(), b.Subscribe()}
	stop := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			b.SendLog(State{}, fmt.Sprintf("line %d", i))
			b.Send(State{Bitrate: i})
		}
	}()
	time.Sleep(10 * time.Millisecond)
	b.Close()
	for _, ch := range chans {
		drainUntilClosed(t, ch)
	}
	close(stop)
	<-sent
}
