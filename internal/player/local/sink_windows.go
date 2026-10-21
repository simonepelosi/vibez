//go:build windows && cgo

package local

/*
#cgo windows LDFLAGS: -lmfuuid -lole32 -lwinmm -static -static-libgcc -static-libstdc++
#include "sink_windows.h"
#include <stdlib.h>

extern void vibezLocalWinOnEOS(uintptr_t userData, uint64_t gen);
extern void vibezLocalWinOnError(uintptr_t userData, uint64_t gen, char* errMsg);

static inline vibez_localsink_t vibez_create_local_win_sink(uintptr_t userData) {
    return vibez_localsink_create(userData, vibezLocalWinOnEOS, vibezLocalWinOnError);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime/cgo"
	"sync"
	"time"
	"unsafe"
)

type nativeEngine struct {
	mu      sync.Mutex
	handle  C.vibez_localsink_t
	cgoH    cgo.Handle
	player  *Player
	gen     uint64
	seq     uint64
	playing bool
	dur     float64
	done    chan struct{}
}

func openNativeEngine(p *Player) (engine, error) {
	e := &nativeEngine{
		player: p,
		done:   make(chan struct{}),
	}
	h := cgo.NewHandle(e)
	e.cgoH = h

	handle := C.vibez_create_local_win_sink(C.uintptr_t(h))
	if handle == nil {
		h.Delete()
		return nil, errors.New("failed to create Windows local audio sink")
	}
	e.handle = handle
	go e.pollLoop()
	return e, nil
}

func engineFromHandle(userData C.uintptr_t) (e *nativeEngine) {
	defer func() { _ = recover() }()
	e, _ = cgo.Handle(userData).Value().(*nativeEngine)
	return e
}

//export vibezLocalWinOnEOS
func vibezLocalWinOnEOS(userData C.uintptr_t, gen C.uint64_t) {
	e := engineFromHandle(userData)
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.handle == nil || uint64(gen) != e.gen {
		e.mu.Unlock()
		return
	}
	e.playing = false
	e.seq++
	snap := snapshot{
		Gen:      e.gen,
		Seq:      e.seq,
		Kind:     "ended",
		Playing:  false,
		Position: e.dur,
		Duration: e.dur,
	}
	e.mu.Unlock()
	go e.player.onEvent(snap)
}

//export vibezLocalWinOnError
func vibezLocalWinOnError(userData C.uintptr_t, gen C.uint64_t, cErrMsg *C.char) {
	msg := C.GoString(cErrMsg)
	e := engineFromHandle(userData)
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.handle == nil || uint64(gen) != e.gen {
		e.mu.Unlock()
		return
	}
	e.playing = false
	e.seq++
	snap := snapshot{
		Gen:      e.gen,
		Seq:      e.seq,
		Kind:     "error",
		Playing:  false,
		Duration: e.dur,
		Error:    msg,
	}
	e.mu.Unlock()
	go e.player.onEvent(snap)
}

func (e *nativeEngine) pollLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.mu.Lock()
			if e.handle == nil || !e.playing || e.gen == 0 {
				e.mu.Unlock()
				continue
			}
			pos := float64(C.vibez_localsink_position(e.handle))
			e.seq++
			snap := snapshot{
				Gen:      e.gen,
				Seq:      e.seq,
				Kind:     "timeupdate",
				Playing:  true,
				Position: pos,
				Duration: e.dur,
			}
			e.mu.Unlock()
			e.player.onEvent(snap)
		case <-e.done:
			return
		}
	}
}

func (e *nativeEngine) call(method string, args ...any) (snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handle == nil {
		return snapshot{}, errors.New("audio sink closed")
	}

	switch method {
	case "load":
		path := args[0].(string)
		gen := args[1].(uint64)
		autoplay := args[2].(bool)
		autoInt := C.int(0)
		if autoplay {
			autoInt = 1
		}
		cs := C.CString(path)
		defer C.free(unsafe.Pointer(cs))
		var dur C.double
		errBuf := make([]byte, 256)
		rc := C.vibez_localsink_load(
			e.handle,
			cs,
			C.uint64_t(gen),
			autoInt,
			&dur,
			(*C.char)(unsafe.Pointer(&errBuf[0])),
			C.int(len(errBuf)),
		)
		e.gen = gen
		e.seq++
		if rc != 0 {
			e.playing = false
			e.dur = 0
			msg := C.GoString((*C.char)(unsafe.Pointer(&errBuf[0])))
			if msg == "" {
				msg = "cannot open audio file"
			}
			return snapshot{Gen: gen, Seq: e.seq, Error: msg}, nil
		}
		e.playing = autoplay
		e.dur = float64(dur)
		return snapshot{Gen: gen, Seq: e.seq, Playing: autoplay, Duration: e.dur}, nil

	case "play":
		C.vibez_localsink_play(e.handle)
		e.playing = true
		pos := float64(C.vibez_localsink_position(e.handle))
		e.seq++
		return snapshot{Gen: e.gen, Seq: e.seq, Playing: true, Position: pos, Duration: e.dur}, nil

	case "pause":
		C.vibez_localsink_pause(e.handle)
		e.playing = false
		pos := float64(C.vibez_localsink_position(e.handle))
		e.seq++
		return snapshot{Gen: e.gen, Seq: e.seq, Playing: false, Position: pos, Duration: e.dur}, nil

	case "stop":
		C.vibez_localsink_stop(e.handle)
		e.playing = false
		e.seq++
		return snapshot{Gen: e.gen, Seq: e.seq, Playing: false, Position: 0, Duration: e.dur}, nil

	case "seek":
		sec := args[0].(float64)
		C.vibez_localsink_seek(e.handle, C.double(sec))
		e.seq++
		return snapshot{Gen: e.gen, Seq: e.seq, Playing: e.playing, Position: sec, Duration: e.dur}, nil

	case "setVolume":
		vol := args[0].(float64)
		C.vibez_localsink_set_volume(e.handle, C.double(vol))
		pos := float64(C.vibez_localsink_position(e.handle))
		e.seq++
		return snapshot{Gen: e.gen, Seq: e.seq, Playing: e.playing, Position: pos, Duration: e.dur}, nil

	case "setEqualizer":
		rawBands := args[0].([]any)
		if len(rawBands) == 0 {
			C.vibez_localsink_set_eq(e.handle, nil, 0)
		} else {
			cg := make([]C.double, len(rawBands))
			for i, b := range rawBands {
				m := b.(map[string]any)
				cg[i] = C.double(m["gain"].(float64))
			}
			C.vibez_localsink_set_eq(e.handle, &cg[0], C.int(len(cg)))
		}
		pos := float64(C.vibez_localsink_position(e.handle))
		e.seq++
		return snapshot{Gen: e.gen, Seq: e.seq, Playing: e.playing, Position: pos, Duration: e.dur}, nil

	case "unload":
		C.vibez_localsink_unload(e.handle)
		e.gen = 0
		e.playing = false
		e.dur = 0
		e.seq++
		return snapshot{Seq: e.seq}, nil

	default:
		return snapshot{}, fmt.Errorf("unknown command: %s", method)
	}
}

func (e *nativeEngine) close() {
	e.mu.Lock()
	handle := e.handle
	e.handle = nil
	e.playing = false
	e.gen = 0
	e.mu.Unlock()
	if handle != nil {
		close(e.done)
		C.vibez_localsink_destroy(handle)
		e.cgoH.Delete()
	}
}
