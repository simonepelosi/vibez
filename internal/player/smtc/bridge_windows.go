//go:build windows && cgo

package smtc

/*
#cgo windows LDFLAGS: -luser32 -lole32 -static -static-libgcc -static-libstdc++
#include "smtc_windows.h"
#include <stdlib.h>

extern void vibezSMTCOnEvent(uintptr_t userData, int event, int64_t positionHns);
extern void vibezSMTCOnError(uintptr_t userData, char* msg);

// The wrappers keep Go pointers out of the calls, so cgo has no pointer to
// check: results come back by value and strings are C allocations.
typedef struct {
    vibez_smtc_t handle;
    int32_t hr;
    const char* stage;
} vibez_smtc_create_result_t;

static inline vibez_smtc_create_result_t vibez_smtc_create_go(uintptr_t userData) {
    vibez_smtc_create_result_t r = {0};
    r.handle = vibez_smtc_create(userData, vibezSMTCOnEvent, vibezSMTCOnError, &r.hr, &r.stage);
    return r;
}

static inline void vibez_smtc_update_go(vibez_smtc_t handle,
                                        int hasMetadata, char* title, char* artist, char* album, char* artworkURL,
                                        int hasStatus, int status,
                                        int hasTimeline, int64_t positionHns, int64_t durationHns) {
    vibez_smtc_update_t u = {0};
    u.has_metadata = hasMetadata;
    u.title = title;
    u.artist = artist;
    u.album = album;
    u.artwork_url = artworkURL;
    u.has_status = hasStatus;
    u.status = status;
    u.has_timeline = hasTimeline;
    u.position_hns = positionHns;
    u.duration_hns = durationHns;
    vibez_smtc_update(handle, &u);
}
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"
)

// nativeBridge drives the SMTC bridge in smtc_windows.cpp.
type nativeBridge struct {
	mu      sync.Mutex
	handle  C.vibez_smtc_t
	cgoH    cgo.Handle
	onEvent func(event)
	onError func(string)
}

// newNativeBridge creates the hidden window and its SMTC session. An error
// carries the failing step and HRESULT.
func newNativeBridge(onEvent func(event), onError func(string)) (bridge, error) {
	b := &nativeBridge{onEvent: onEvent, onError: onError}
	b.cgoH = cgo.NewHandle(b)
	r := C.vibez_smtc_create_go(C.uintptr_t(b.cgoH))
	if r.handle == nil {
		b.cgoH.Delete()
		return nil, fmt.Errorf("smtc: %s failed: HRESULT 0x%08X", C.GoString(r.stage), uint32(r.hr))
	}
	b.handle = r.handle
	return b, nil
}

// bridgeFromHandle resolves a callback's handle. close waits out running
// callbacks before it deletes the handle, so a stale one should not reach
// here; recovering keeps a late one harmless all the same.
func bridgeFromHandle(userData C.uintptr_t) (b *nativeBridge) {
	defer func() { _ = recover() }()
	b, _ = cgo.Handle(userData).Value().(*nativeBridge)
	return b
}

//export vibezSMTCOnEvent
func vibezSMTCOnEvent(userData C.uintptr_t, ev C.int, positionHns C.int64_t) {
	b := bridgeFromHandle(userData)
	if b == nil {
		return
	}
	var e event
	switch ev {
	case C.VIBEZ_SMTC_EVENT_PLAY:
		e.kind = eventPlay
	case C.VIBEZ_SMTC_EVENT_PAUSE:
		e.kind = eventPause
	case C.VIBEZ_SMTC_EVENT_STOP:
		e.kind = eventStop
	case C.VIBEZ_SMTC_EVENT_NEXT:
		e.kind = eventNext
	case C.VIBEZ_SMTC_EVENT_PREVIOUS:
		e.kind = eventPrevious
	case C.VIBEZ_SMTC_EVENT_SEEK:
		e.kind = eventSeek
		e.position = time.Duration(positionHns) * 100
	default:
		return
	}
	b.onEvent(e)
}

//export vibezSMTCOnError
func vibezSMTCOnError(userData C.uintptr_t, msg *C.char) {
	if b := bridgeFromHandle(userData); b != nil {
		b.onError(C.GoString(msg))
	}
}

// cstr converts s for the native side. A NUL would end the C string early, and
// SMTC strings cannot hold one anyway, so it is dropped.
func cstr(s string) *C.char {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			s = s[:i]
			break
		}
	}
	return C.CString(s)
}

func (b *nativeBridge) apply(u update) {
	var (
		hasMeta, hasStatus, hasTimeline C.int
		title, artist, album, artwork   *C.char
		status                          C.int
		pos, dur                        C.int64_t
	)
	if m := u.Metadata; m != nil {
		hasMeta = 1
		title, artist, album, artwork = cstr(m.Title), cstr(m.Artist), cstr(m.Album), cstr(m.ArtworkURL)
		defer C.free(unsafe.Pointer(title))
		defer C.free(unsafe.Pointer(artist))
		defer C.free(unsafe.Pointer(album))
		defer C.free(unsafe.Pointer(artwork))
	}
	if u.Status != nil {
		hasStatus = 1
		status = C.int(*u.Status)
	}
	if tl := u.Timeline; tl != nil {
		hasTimeline = 1
		pos = C.int64_t(tl.Position / 100)
		dur = C.int64_t(tl.Duration / 100)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handle != nil {
		C.vibez_smtc_update_go(b.handle, hasMeta, title, artist, album, artwork, hasStatus, status, hasTimeline, pos, dur)
	}
}

func (b *nativeBridge) close() {
	b.mu.Lock()
	handle := b.handle
	b.handle = nil
	b.mu.Unlock()
	if handle != nil {
		C.vibez_smtc_destroy(handle)
		b.cgoH.Delete()
	}
}
