//go:build linux

// Package gst provides a GStreamer-backed audio player for URI-based streams.
// It wraps a playbin3 (or playbin fallback) pipeline via CGO, handling HLS
// manifests, plain HTTPS audio files, and eventual authenticated streams.
package gst

/*
#cgo pkg-config: gstreamer-1.0
#include <gst/gst.h>
#include <stdlib.h>

static void vibez_gst_init(void) {
    if (!gst_is_initialized())
        gst_init(NULL, NULL);
}

// vibez_gst_new creates a playbin3 pipeline, falling back to playbin.
static GstElement* vibez_gst_new(void) {
    GstElement* p = gst_element_factory_make("playbin3", "vibez-audio");
    if (!p) p = gst_element_factory_make("playbin", "vibez-audio");
    return p;
}

static void vibez_gst_set_uri(GstElement* p, const char* uri) {
    // Use READY (not NULL) to release the current media while keeping the
    // pipeline and its PipeWire/PulseAudio connection alive. Going to NULL
    // would tear down the audio sink and create a new OS-level player entry
    // on every track change, causing dozens of stale entries in notification
    // centres that track MPRIS/PipeWire streams.
    gst_element_set_state(p, GST_STATE_READY);
    g_object_set(G_OBJECT(p), "uri", uri, NULL);
}

static void vibez_gst_play(GstElement* p)  { gst_element_set_state(p, GST_STATE_PLAYING); }
static void vibez_gst_pause(GstElement* p) { gst_element_set_state(p, GST_STATE_PAUSED);  }
static void vibez_gst_stop(GstElement* p)  { gst_element_set_state(p, GST_STATE_NULL);    }

static void vibez_gst_seek(GstElement* p, gint64 pos_ns) {
    gst_element_seek_simple(p, GST_FORMAT_TIME,
        (GstSeekFlags)(GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_KEY_UNIT), pos_ns);
}

static void vibez_gst_set_volume(GstElement* p, gdouble v) {
    g_object_set(G_OBJECT(p), "volume", v, NULL);
}

static gint64 vibez_gst_position(GstElement* p) {
    gint64 v = GST_CLOCK_TIME_NONE;
    gst_element_query_position(p, GST_FORMAT_TIME, &v);
    return v;
}

static gint64 vibez_gst_duration(GstElement* p) {
    gint64 v = GST_CLOCK_TIME_NONE;
    gst_element_query_duration(p, GST_FORMAT_TIME, &v);
    return v;
}

// vibez_gst_pop_bus pops one message from the pipeline bus.
// Returns the GstMessageType (0 = no message).
// For ERROR messages, *errOut is set to a g_strdup'd string (caller frees).
static gint vibez_gst_pop_bus(GstElement* p, gchar** errOut) {
    GstBus* bus = gst_element_get_bus(p);
    if (!bus) return 0;
    GstMessage* msg = gst_bus_pop(bus);
    gst_object_unref(bus);
    if (!msg) return 0;
    gint t = (gint)GST_MESSAGE_TYPE(msg);
    if (t == (gint)GST_MESSAGE_ERROR && errOut) {
        GError* e = NULL;
        gst_message_parse_error(msg, &e, NULL);
        if (e) { *errOut = g_strdup(e->message); g_error_free(e); }
    }
    gst_message_unref(msg);
    return t;
}

static void vibez_gst_free(gchar* s) { g_free(s); }

static void vibez_gst_destroy(GstElement* p) {
    gst_element_set_state(p, GST_STATE_NULL);
    gst_object_unref(p);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

var once sync.Once

// Player wraps a GStreamer playbin3 pipeline for URI-based audio playback.
type Player struct {
	mu       sync.Mutex
	pipeline *C.GstElement
	onEOS    func()
	onError  func(error)
	stop     chan struct{}
	wg       sync.WaitGroup
}

// New initialises GStreamer once and returns a ready Player.
func New() (*Player, error) {
	once.Do(func() { C.vibez_gst_init() })
	pl := C.vibez_gst_new()
	if pl == nil {
		return nil, fmt.Errorf("gst: playbin3/playbin element unavailable — is gstreamer1.0-plugins-base installed?")
	}
	p := &Player{pipeline: pl, stop: make(chan struct{})}
	p.wg.Add(1)
	go p.busLoop()
	return p, nil
}

// OnEOS registers a callback fired (in a goroutine) when the stream ends.
func (p *Player) OnEOS(fn func()) {
	p.mu.Lock()
	p.onEOS = fn
	p.mu.Unlock()
}

// OnError registers a callback fired (in a goroutine) on pipeline error.
func (p *Player) OnError(fn func(error)) {
	p.mu.Lock()
	p.onError = fn
	p.mu.Unlock()
}

// PlayURI loads the given URI (https:// or file://) and starts playback.
func (p *Player) PlayURI(uri string) {
	cs := C.CString(uri)
	defer C.free(unsafe.Pointer(cs))
	p.mu.Lock()
	C.vibez_gst_set_uri(p.pipeline, cs)
	C.vibez_gst_play(p.pipeline)
	p.mu.Unlock()
}

// Play resumes a paused pipeline.
func (p *Player) Play() {
	p.mu.Lock()
	C.vibez_gst_play(p.pipeline)
	p.mu.Unlock()
}

// Pause pauses the pipeline.
func (p *Player) Pause() {
	p.mu.Lock()
	C.vibez_gst_pause(p.pipeline)
	p.mu.Unlock()
}

// Stop resets the pipeline to NULL state, releasing network connections.
func (p *Player) Stop() {
	p.mu.Lock()
	C.vibez_gst_stop(p.pipeline)
	p.mu.Unlock()
}

// Seek seeks to the given position within the current stream.
func (p *Player) Seek(pos time.Duration) {
	p.mu.Lock()
	C.vibez_gst_seek(p.pipeline, C.gint64(pos.Nanoseconds()))
	p.mu.Unlock()
}

// SetVolume sets playback volume in [0.0, 1.0].
func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	C.vibez_gst_set_volume(p.pipeline, C.gdouble(v))
	p.mu.Unlock()
}

// Position returns the current playback position (0 if unknown).
func (p *Player) Position() time.Duration {
	p.mu.Lock()
	ns := int64(C.vibez_gst_position(p.pipeline))
	p.mu.Unlock()
	if ns < 0 {
		return 0
	}
	return time.Duration(ns)
}

// Duration returns total stream duration (0 if unknown or live).
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	ns := int64(C.vibez_gst_duration(p.pipeline))
	p.mu.Unlock()
	if ns < 0 {
		return 0
	}
	return time.Duration(ns)
}

// Destroy shuts down the bus poller and frees the GStreamer pipeline.
func (p *Player) Destroy() {
	close(p.stop)
	p.wg.Wait()
	p.mu.Lock()
	C.vibez_gst_destroy(p.pipeline)
	p.mu.Unlock()
}

func (p *Player) busLoop() {
	defer p.wg.Done()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-tick.C:
			p.drainBus()
		}
	}
}

func (p *Player) drainBus() {
	for {
		var errMsg *C.gchar
		p.mu.Lock()
		t := C.vibez_gst_pop_bus(p.pipeline, &errMsg) //nolint:gocritic // false positive: CGO AST confuses dupSubExpr
		p.mu.Unlock()

		if t == 0 {
			return
		}
		switch C.GstMessageType(t) {
		case C.GST_MESSAGE_EOS:
			p.mu.Lock()
			cb := p.onEOS
			p.mu.Unlock()
			if cb != nil {
				go cb()
			}
		case C.GST_MESSAGE_ERROR:
			var msg string
			if errMsg != nil {
				msg = C.GoString((*C.char)(unsafe.Pointer(errMsg)))
				C.vibez_gst_free(errMsg)
			}
			p.mu.Lock()
			cb := p.onError
			p.mu.Unlock()
			if cb != nil {
				go cb(fmt.Errorf("gst: %s", msg))
			}
		}
	}
}
