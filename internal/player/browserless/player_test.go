//go:build linux

package browserless

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider"
)

func TestPlayerQueueIndexAdjustments(t *testing.T) {
	tracks := []provider.Track{
		{ID: "0"},
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
	}

	p := &Player{
		queue: tracks,
		idx:   2, // playing track "2"
	}

	// Move playing item (index 2) to 0
	_ = p.MoveInQueue(2, 0)
	if p.idx != 0 {
		t.Errorf("expected idx 0 after moving playing item to 0, got %d", p.idx)
	}
	if p.queue[p.idx].ID != "2" {
		t.Errorf("expected playing track to remain ID '2', got ID %s", p.queue[p.idx].ID)
	}

	// Queue is now: [2, 0, 1, 3], idx is 0 ("2")
	// Move item from 3 ("3") to 0. It shifts playing item rightwards!
	_ = p.MoveInQueue(3, 0)
	// Queue is now: [3, 2, 0, 1], idx should have incremented to 1
	if p.idx != 1 {
		t.Errorf("expected idx 1 after inserting before playing item, got %d", p.idx)
	}
	if p.queue[p.idx].ID != "2" {
		t.Errorf("expected playing track to remain ID '2', got ID %s", p.queue[p.idx].ID)
	}

	// Move item from 0 ("3") to 3 (after playing item). It shifts playing item leftwards!
	_ = p.MoveInQueue(0, 3)
	// Queue is now: [2, 0, 3, 1] or similar, idx should have decremented to 0
	if p.idx != 0 {
		t.Errorf("expected idx 0 after moving item past playing item, got %d", p.idx)
	}
	if p.queue[p.idx].ID != "2" {
		t.Errorf("expected playing track to remain ID '2', got ID %s", p.queue[p.idx].ID)
	}

	// Remove item before playing index
	p.idx = 2 // say playing item at index 2
	_ = p.RemoveFromQueue(0)
	// Item before index removed, playing item shifts left: idx becomes 1
	if p.idx != 1 {
		t.Errorf("expected idx 1 after removing prior item, got %d", p.idx)
	}

	// Remove current playing item
	_ = p.RemoveFromQueue(1)
	if p.state.Track != nil {
		t.Errorf("expected Track to be cleared after removing playing item, got %+v", p.state.Track)
	}
}

func TestStreamServerSecurity(t *testing.T) {
	dummyHex := "0123456789abcdef0123456789abcdef"
	s := &StreamServer{
		authToken: dummyHex,
		streams:   make(map[string]*trackStream),
	}

	// Non-loopback RemoteAddr should be forbidden
	req := httptest.NewRequest(http.MethodGet, "/stream/fake/"+dummyHex+".aac", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rr := httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-loopback remote addr, got %d", rr.Code)
	}

	// Foreign Host header should be forbidden
	req = httptest.NewRequest(http.MethodGet, "/stream/fake/"+dummyHex+".aac", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "evil-rebinding-domain.com"
	rr = httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for invalid host header, got %d", rr.Code)
	}

	// Valid host + loopback, but invalid auth token
	req = httptest.NewRequest(http.MethodGet, "/stream/fake/wrong-hex.aac", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1:8080"
	rr = httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for wrong token, got %d", rr.Code)
	}

	// Valid host, loopback, and auth token, but stream does not exist
	req = httptest.NewRequest(http.MethodGet, "/stream/fake/"+dummyHex+".aac", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1:8080"
	rr = httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for non-existent stream, got %d", rr.Code)
	}
}
