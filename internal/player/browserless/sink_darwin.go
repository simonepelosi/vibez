//go:build darwin && cgo

package browserless

/*
#cgo darwin CFLAGS: -fobjc-arc
#cgo darwin LDFLAGS: -framework AVFoundation -framework Foundation -framework CoreMedia
#include "sink_darwin.h"
#include <stdlib.h>

extern void vibezDarwinOnEOS(void* userData);
extern void vibezDarwinOnError(void* userData, const char* errMsg);

static inline vibez_avplayer_t vibez_create_darwin_sink(void* userData) {
    return vibez_avplayer_create(userData, vibezDarwinOnEOS, vibezDarwinOnError);
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
	handle  C.vibez_avplayer_t
	cgoH    cgo.Handle
	onEOS   func()
	onError func(error)
}

func newAudioSink() (audioSink, error) {
	s := &darwinAudioSink{}
	h := cgo.NewHandle(s)
	s.cgoH = h

	playerHandle := C.vibez_create_darwin_sink(unsafe.Pointer(uintptr(h)))
	if playerHandle == nil {
		h.Delete()
		return nil, errors.New("failed to create AVPlayer audio sink")
	}
	s.handle = playerHandle
	return s, nil
}

//export vibezDarwinOnEOS
func vibezDarwinOnEOS(userData unsafe.Pointer) {
	h := cgo.Handle(uintptr(userData))
	if s, ok := h.Value().(*darwinAudioSink); ok {
		s.mu.Lock()
		cb := s.onEOS
		s.mu.Unlock()
		if cb != nil {
			go cb()
		}
	}
}

//export vibezDarwinOnError
func vibezDarwinOnError(userData unsafe.Pointer, cErrMsg *C.char) {
	msg := C.GoString(cErrMsg)
	h := cgo.Handle(uintptr(userData))
	if s, ok := h.Value().(*darwinAudioSink); ok {
		s.mu.Lock()
		cb := s.onError
		s.mu.Unlock()
		if cb != nil {
			go cb(errors.New(msg))
		}
	}
}

func (s *darwinAudioSink) PlayURI(uri string) {
	cs := C.CString(uri)
	defer C.free(unsafe.Pointer(cs))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_play_uri(s.handle, cs)
	}
}

func (s *darwinAudioSink) Play() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_play(s.handle)
	}
}

func (s *darwinAudioSink) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_pause(s.handle)
	}
}

func (s *darwinAudioSink) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_stop(s.handle)
	}
}

func (s *darwinAudioSink) Seek(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_seek(s.handle, C.double(d.Seconds()))
	}
}

func (s *darwinAudioSink) SetVolume(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_set_volume(s.handle, C.float(v))
	}
}

func (s *darwinAudioSink) Position() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != nil {
		sec := C.vibez_avplayer_position(s.handle)
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
	defer s.mu.Unlock()
	if s.handle != nil {
		C.vibez_avplayer_destroy(s.handle)
		s.handle = nil
		s.cgoH.Delete()
	}
}
