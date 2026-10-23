//go:build windows && cgo

package browserless

/*
#cgo windows LDFLAGS: -lmfuuid -lole32 -lwinmm -static -static-libgcc -static-libstdc++
#include "sink_windows.h"
#include <stdlib.h>

extern void vibezWinOnEOS(uintptr_t userData);
extern void vibezWinOnError(uintptr_t userData, char* errMsg);
extern void vibezWinOnStreamStart(uintptr_t userData, char* uri);

static inline vibez_winsink_t vibez_create_win_sink(uintptr_t userData) {
    return vibez_winsink_create(userData, vibezWinOnEOS, vibezWinOnError, vibezWinOnStreamStart);
}
*/
import "C"

import (
	"errors"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"
)

// windowsAudioSink plays the loopback AAC stream with Media Foundation and
// waveOut (see sink_windows.cpp).
type windowsAudioSink struct {
	mu            sync.Mutex
	handle        C.vibez_winsink_t
	cgoH          cgo.Handle
	onEOS         func()
	onError       func(error)
	onStreamStart func(string)
}

func newAudioSink() (audioSink, error) {
	s := &windowsAudioSink{}
	h := cgo.NewHandle(s)
	s.cgoH = h

	handle := C.vibez_create_win_sink(C.uintptr_t(h))
	if handle == nil {
		h.Delete()
		return nil, errors.New("failed to create Windows audio sink")
	}
	s.handle = handle
	return s, nil
}

// sinkFromHandle resolves a callback's handle. The native threads can fire a
// callback just as Destroy deletes the handle, so a stale handle is not an error.
func sinkFromHandle(userData C.uintptr_t) (s *windowsAudioSink) {
	defer func() { _ = recover() }()
	s, _ = cgo.Handle(userData).Value().(*windowsAudioSink)
	return s
}

//export vibezWinOnEOS
func vibezWinOnEOS(userData C.uintptr_t) {
	s := sinkFromHandle(userData)
	if s == nil {
		return
	}
	s.mu.Lock()
	cb := s.onEOS
	s.mu.Unlock()
	if cb != nil {
		go cb()
	}
}

//export vibezWinOnError
func vibezWinOnError(userData C.uintptr_t, cErrMsg *C.char) {
	msg := C.GoString(cErrMsg)
	s := sinkFromHandle(userData)
	if s == nil {
		return
	}
	s.mu.Lock()
	cb := s.onError
	s.mu.Unlock()
	if cb != nil {
		go cb(errors.New(msg))
	}
}

//export vibezWinOnStreamStart
func vibezWinOnStreamStart(userData C.uintptr_t, cURI *C.char) {
	uri := C.GoString(cURI)
	s := sinkFromHandle(userData)
	if s == nil {
		return
	}
	s.mu.Lock()
	cb := s.onStreamStart
	s.mu.Unlock()
	if cb != nil {
		go cb(uri)
	}
}

func (s *windowsAudioSink) PlayURI(uri string) {
	cs := C.CString(uri)
	defer C.free(unsafe.Pointer(cs))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_play_uri(s.handle, cs)
	}
}

func (s *windowsAudioSink) SetNextURI(uri string) {
	var cs *C.char
	if uri != "" {
		cs = C.CString(uri)
		defer C.free(unsafe.Pointer(cs))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_set_next_uri(s.handle, cs)
	}
}

func (s *windowsAudioSink) OnStreamStart(fn func(string)) {
	s.mu.Lock()
	s.onStreamStart = fn
	s.mu.Unlock()
}
func (s *windowsAudioSink) Play() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_play(s.handle)
	}
}

func (s *windowsAudioSink) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_pause(s.handle)
	}
}

func (s *windowsAudioSink) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_stop(s.handle)
	}
}

// Seek is a no-op: the player seeks by restarting the stream at a segment
// boundary (PlayURI with ?start_seg=N), as the loopback stream is not seekable.
func (s *windowsAudioSink) Seek(_ time.Duration) {}

func (s *windowsAudioSink) SetVolume(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_set_volume(s.handle, C.double(v))
	}
}

func (s *windowsAudioSink) SetEqualizer(gains []float64) {
	if len(gains) == 0 {
		return
	}
	cg := make([]C.double, len(gains))
	for i, g := range gains {
		cg[i] = C.double(g)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_winsink_set_eq(s.handle, &cg[0], C.int(len(cg)))
	}
}

func (s *windowsAudioSink) Position() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle == nil {
		return 0
	}
	sec := float64(C.vibez_winsink_position(s.handle))
	return time.Duration(sec * float64(time.Second))
}

func (s *windowsAudioSink) OnEOS(fn func()) {
	s.mu.Lock()
	s.onEOS = fn
	s.mu.Unlock()
}

func (s *windowsAudioSink) OnError(fn func(error)) {
	s.mu.Lock()
	s.onError = fn
	s.mu.Unlock()
}

func (s *windowsAudioSink) Destroy() {
	s.mu.Lock()
	handle := s.handle
	s.handle = nil
	s.mu.Unlock()
	if handle != nil {
		C.vibez_winsink_destroy(handle)
		s.cgoH.Delete()
	}
}
