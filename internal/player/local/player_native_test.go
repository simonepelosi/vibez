//go:build (linux || darwin) && cgo

package local

import (
	"testing"
	"time"
)

func TestCloseIsIdempotent(t *testing.T) {
	p, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Match a TUI quit followed by the caller's deferred cleanup.
	defer func() { _ = p.Close() }()

	const callers = 8
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- p.Close()
		}()
	}
	close(start)
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for range callers {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-timeout.C:
			t.Fatal("concurrent shutdown did not finish")
		}
	}
	select {
	case <-p.done:
	default:
		t.Fatal("Close did not signal the player workers to stop")
	}
}
