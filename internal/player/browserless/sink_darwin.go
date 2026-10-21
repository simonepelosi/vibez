//go:build darwin && cgo

package browserless

/*
#cgo darwin CXXFLAGS: -std=c++17
#cgo darwin LDFLAGS: -framework AudioToolbox -framework CoreFoundation -lc++
#include "sink_darwin.h"
#include <stdlib.h>

extern void vibezDarwinOnEOS(uintptr_t userData);
extern void vibezDarwinOnError(uintptr_t userData, char* errMsg);

static inline vibez_darwinsink_t vibez_create_darwin_sink(uintptr_t userData) {
    return vibez_darwinsink_create(userData, vibezDarwinOnEOS, vibezDarwinOnError);
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

type darwinAudioSink struct {
	mu      sync.Mutex
	handle  C.vibez_darwinsink_t
	cgoH    cgo.Handle
	onEOS   func()
	onError func(error)
}

func newAudioSink() (audioSink, error) {
	s := &darwinAudioSink{}
	h := cgo.NewHandle(s)
	s.cgoH = h

	playerHandle := C.vibez_create_darwin_sink(C.uintptr_t(h))
	if playerHandle == nil {
		h.Delete()
		return nil, errors.New("failed to create Darwin audio sink")
	}
	s.handle = playerHandle
	return s, nil
}

func sinkFromHandle(userData C.uintptr_t) (s *darwinAudioSink) {
	defer func() { _ = recover() }()
	s, _ = cgo.Handle(userData).Value().(*darwinAudioSink)
	return s
}

//export vibezDarwinOnEOS
func vibezDarwinOnEOS(userData C.uintptr_t) {
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

//export vibezDarwinOnError
func vibezDarwinOnError(userData C.uintptr_t, cErrMsg *C.char) {
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

func (s *darwinAudioSink) PlayURI(uri string) {
	cs := C.CString(uri)
	defer C.free(unsafe.Pointer(cs))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_darwinsink_play_uri(s.handle, cs)
	}
}

func (s *darwinAudioSink) Play() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_darwinsink_play(s.handle)
	}
}

func (s *darwinAudioSink) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_darwinsink_pause(s.handle)
	}
}

func (s *darwinAudioSink) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_darwinsink_stop(s.handle)
	}
}

func (s *darwinAudioSink) Seek(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_darwinsink_seek(s.handle, C.double(d.Seconds()))
	}
}

func (s *darwinAudioSink) SetVolume(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_darwinsink_set_volume(s.handle, C.float(v))
	}
}

func (s *darwinAudioSink) SetEqualizer(_ []float64) {
	// Equalizer on compressed AAC AudioQueue is a no-op.
}

func (s *darwinAudioSink) Position() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		sec := C.vibez_darwinsink_position(s.handle)
		return time.Duration(float64(sec) * float64(time.Second))
	}
	return 0
}

func (s *darwinAudioSink) OnEOS(fn func()) {
	s.mu.Lock()
	s.onEOS = fn
	s.mu.Unlock()
}

func (s *darwinAudioSink) OnError(fn func(error)) {
	s.mu.Lock()
	s.onError = fn
	s.mu.Unlock()
}

func (s *darwinAudioSink) Destroy() {
	s.mu.Lock()
	handle := s.handle
	s.handle = nil
	s.mu.Unlock()
	if handle != nil {
		C.vibez_darwinsink_destroy(handle)
		s.cgoH.Delete()
	}
}
