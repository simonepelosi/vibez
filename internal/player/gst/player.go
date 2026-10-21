//go:build linux

// Package gst provides a GStreamer-backed audio player for URI-based streams.
// It wraps a playbin3 (or playbin fallback) pipeline via CGO, handling HLS
// manifests, plain HTTPS audio files, and eventual authenticated streams.
package gst

/*
#cgo pkg-config: gstreamer-1.0
#include <gst/gst.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    GMutex mu;
    gchar* next_uri;
} VibezGstState;

static void on_about_to_finish(GstElement* playbin, gpointer user_data) {
    VibezGstState* st = (VibezGstState*)user_data;
    if (!st) return;
    gchar* uri = NULL;
    g_mutex_lock(&st->mu);
    if (st->next_uri && st->next_uri[0] != '\0') {
        uri = st->next_uri;
        st->next_uri = NULL;
    }
    g_mutex_unlock(&st->mu);
    if (!uri) return;

    g_object_set(G_OBJECT(playbin), "uri", uri, NULL);
    GstStructure* s = gst_structure_new("vibez-about-to-finish",
        "uri", G_TYPE_STRING, uri, NULL);
    gst_element_post_message(playbin, gst_message_new_application(GST_OBJECT(playbin), s));
    g_free(uri);
}

static VibezGstState* vibez_gst_state_new(void) {
    VibezGstState* st = g_new0(VibezGstState, 1);
    g_mutex_init(&st->mu);
    return st;
}

static void vibez_gst_state_set_next_uri(VibezGstState* st, const char* uri) {
    if (!st) return;
    g_mutex_lock(&st->mu);
    g_free(st->next_uri);
    st->next_uri = (uri && uri[0] != '\0') ? g_strdup(uri) : NULL;
    g_mutex_unlock(&st->mu);
}

static void vibez_gst_state_free(VibezGstState* st) {
    if (!st) return;
    g_mutex_lock(&st->mu);
    g_free(st->next_uri);
    st->next_uri = NULL;
    g_mutex_unlock(&st->mu);
    g_mutex_clear(&st->mu);
    g_free(st);
}
static void vibez_gst_init(void) {
    if (!gst_is_initialized())
        gst_init(NULL, NULL);
}

static int vibez_gst_has_aac_decoder(void) {
    const char* candidates[] = {"avdec_aac", "faad", "fdkaacdec", NULL};
    for (int i = 0; candidates[i] != NULL; i++) {
        GstElementFactory* f = gst_element_factory_find(candidates[i]);
        if (f) {
            gst_object_unref(f);
            return 1;
        }
    }
    return 0;
}

// vibez_gst_new creates a playbin3 pipeline, falling back to playbin, and
// attaches a 10-band equalizer audio-filter when gstreamer1.0-plugins-good is installed.
static GstElement* vibez_gst_new(GstElement** eqOut, VibezGstState* st) {
    GstElement* p = gst_element_factory_make("playbin3", "vibez-audio");
    if (!p) p = gst_element_factory_make("playbin", "vibez-audio");
    if (p) {
        g_signal_connect(p, "about-to-finish", G_CALLBACK(on_about_to_finish), (gpointer)st);
        const char* null_env = getenv("VIBEZ_NULL_AUDIO");
        if (null_env && strcmp(null_env, "1") == 0) {
            GstElement* fake = gst_element_factory_make("fakesink", "vibez-nullsink");
            if (fake) {
                g_object_set(G_OBJECT(fake), "sync", (gboolean)TRUE, NULL);
                g_object_set(G_OBJECT(p), "audio-sink", fake, NULL);
            }
        }
        if (eqOut) {
            GstElement* eq = gst_element_factory_make("equalizer-10bands", "vibez-eq");
            if (eq) {
                g_object_set(G_OBJECT(p), "audio-filter", eq, NULL);
                *eqOut = eq;
            }
        }
    }
    return p;
}

static void vibez_gst_set_eq_band(GstElement* eq, gint idx, gdouble gain) {
    if (!eq || idx < 0 || idx > 9) return;
    char prop[8];
    g_snprintf(prop, sizeof(prop), "band%d", idx);
    if (gain < -24.0) gain = -24.0;
    if (gain > 12.0) gain = 12.0;
    g_object_set(G_OBJECT(eq), prop, gain, NULL);
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
// For ERROR or APPLICATION messages, *strOut is set to a g_strdup'd string (caller frees).
static gint vibez_gst_pop_bus(GstElement* p, gchar** strOut) {
    GstBus* bus = gst_element_get_bus(p);
    if (!bus) return 0;
    GstMessage* msg = gst_bus_pop(bus);
    gst_object_unref(bus);
    if (!msg) return 0;
    gint t = (gint)GST_MESSAGE_TYPE(msg);
    if (t == (gint)GST_MESSAGE_STREAM_START) {
        if (GST_MESSAGE_SRC(msg) != GST_OBJECT(p)) {
            t = -1; // ignore internal child element stream-start
        }
    } else if (t == (gint)GST_MESSAGE_APPLICATION && strOut) {
        const GstStructure* s = gst_message_get_structure(msg);
        if (s && gst_structure_has_name(s, "vibez-about-to-finish")) {
            const gchar* u = gst_structure_get_string(s, "uri");
            if (u) *strOut = g_strdup(u);
        } else {
            t = -1;
        }
    } else if (t == (gint)GST_MESSAGE_ERROR && strOut) {
        GError* e = NULL;
        gst_message_parse_error(msg, &e, NULL);
        if (e) { *strOut = g_strdup(e->message); g_error_free(e); }
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
	"strings"
	"sync"
	"time"
	"unsafe"
)

const aacDecoderHelp = "install gstreamer1.0-libav or gstreamer1.0-plugins-bad (Debian/Ubuntu) / gst-libav or gst-plugins-bad (Arch/Fedora)"

var once sync.Once

// Player wraps a GStreamer playbin3 pipeline for URI-based audio playback.
type Player struct {
	mu            sync.Mutex
	pipeline      *C.GstElement
	eq            *C.GstElement
	st            *C.VibezGstState
	committedURI  string
	onEOS         func()
	onError       func(error)
	onStreamStart func(string)
	stop          chan struct{}
	wg            sync.WaitGroup
}

// New initialises GStreamer once and returns a ready Player.
func New() (*Player, error) {
	once.Do(func() { C.vibez_gst_init() })
	st := C.vibez_gst_state_new()
	p := &Player{
		st:   st,
		stop: make(chan struct{}),
	}

	var eq *C.GstElement
	pl := C.vibez_gst_new(&eq, st) //nolint:gocritic // CGO wrapper triggers false-positive dupSubExpr
	if pl == nil {
		C.vibez_gst_state_free(st)
		return nil, fmt.Errorf("gst: playbin3/playbin element unavailable — is gstreamer1.0-plugins-base installed?")
	}
	p.pipeline = pl
	p.eq = eq
	p.wg.Add(1)
	go p.busLoop()
	return p, nil
}

// EnsureAACDecoder verifies that a GStreamer AAC decoder plugin (avdec_aac,
// faad, or fdkaacdec) is installed for Apple Music AAC streaming.
func EnsureAACDecoder() error {
	once.Do(func() { C.vibez_gst_init() })
	if C.vibez_gst_has_aac_decoder() == 0 {
		return fmt.Errorf("gst: no GStreamer AAC decoder plugin found — %s", aacDecoderHelp)
	}
	return nil
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

// OnStreamStart registers a callback fired (in a goroutine) with the URI of the
// gaplessly queued track when it begins playing.
func (p *Player) OnStreamStart(fn func(string)) {
	p.mu.Lock()
	p.onStreamStart = fn
	p.mu.Unlock()
}

// SetNextURI pre-stages the next URI to be handed to playbin on about-to-finish
// for gapless track transitions. Pass "" to clear any staged URI.
func (p *Player) SetNextURI(uri string) {
	var cs *C.char
	if uri != "" {
		cs = C.CString(uri)
		defer C.free(unsafe.Pointer(cs))
	}
	p.mu.Lock()
	if p.st != nil {
		C.vibez_gst_state_set_next_uri(p.st, cs)
	}
	p.mu.Unlock()
}

// PlayURI loads the given URI (https:// or file://) and starts playback.
func (p *Player) PlayURI(uri string) {
	cs := C.CString(uri)
	defer C.free(unsafe.Pointer(cs))
	p.mu.Lock()
	if p.st != nil {
		C.vibez_gst_state_set_next_uri(p.st, nil)
	}
	p.committedURI = ""
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
	if p.st != nil {
		C.vibez_gst_state_set_next_uri(p.st, nil)
	}
	p.committedURI = ""
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

// SetEqualizer sets the 10-band equalizer gains in dB (clamped to [-24, +12]).
// Pass nil or an empty slice to reset all 10 bands to 0 dB.
func (p *Player) SetEqualizer(gains []float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eq == nil {
		return
	}
	for i := range 10 {
		gain := 0.0
		if i < len(gains) {
			gain = gains[i]
		}
		C.vibez_gst_set_eq_band(p.eq, C.gint(i), C.gdouble(gain))
	}
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
	p.pipeline = nil
	C.vibez_gst_state_free(p.st)
	p.st = nil
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
		var strOut *C.gchar
		p.mu.Lock()
		t := C.vibez_gst_pop_bus(p.pipeline, &strOut) //nolint:gocritic // false positive: CGO AST confuses dupSubExpr
		p.mu.Unlock()

		if t == 0 {
			return
		}
		if t < 0 {
			if strOut != nil {
				C.vibez_gst_free(strOut)
			}
			continue
		}
		switch C.GstMessageType(t) {
		case C.GST_MESSAGE_APPLICATION:
			var uri string
			if strOut != nil {
				uri = C.GoString((*C.char)(unsafe.Pointer(strOut)))
				C.vibez_gst_free(strOut)
			}
			if uri != "" {
				p.mu.Lock()
				p.committedURI = uri
				p.mu.Unlock()
			}
		case C.GST_MESSAGE_STREAM_START:
			p.mu.Lock()
			var cb func(string)
			uri := p.committedURI
			if uri != "" {
				p.committedURI = ""
				cb = p.onStreamStart
			}
			p.mu.Unlock()
			if cb != nil {
				go cb(uri)
			}
		case C.GST_MESSAGE_EOS:
			p.mu.Lock()
			cb := p.onEOS
			p.mu.Unlock()
			if cb != nil {
				go cb()
			}
		case C.GST_MESSAGE_ERROR:
			var msg string
			if strOut != nil {
				msg = C.GoString((*C.char)(unsafe.Pointer(strOut)))
				C.vibez_gst_free(strOut)
			}
			if strings.Contains(msg, "No suitable plugins found") || strings.Contains(msg, "missing a plug-in") {
				msg = fmt.Sprintf("%s (%s)", msg, aacDecoderHelp)
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
