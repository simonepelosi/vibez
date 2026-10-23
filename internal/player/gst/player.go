//go:build linux

// Package gst provides a GStreamer-backed audio player for URI-based streams.
// It wraps a playbin3 (or playbin fallback) pipeline via CGO, handling HLS
// manifests, plain HTTPS audio files, and eventual authenticated streams.
package gst

/*
#cgo pkg-config: gstreamer-1.0
#cgo LDFLAGS: -lm
#include <gst/gst.h>
#include <arpa/inet.h>
#include <math.h>
#include <netinet/in.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#define VIBEZ_EQ_BANDS 10

static const double vibez_eq_freqs[VIBEZ_EQ_BANDS] = {
    29.0, 59.0, 119.0, 237.0, 474.0, 947.0, 1889.0, 3770.0, 7523.0, 15011.0
};

typedef struct {
    double b0, b1, b2, a1, a2;
    double z1[2], z2[2];
    gboolean active;
} VibezBiquad;

static void vibez_biquad_design(VibezBiquad* bq, double fs, double f0, double gain_db) {
    if (fabs(gain_db) < 0.01 || fs <= 0.0 || f0 >= fs * 0.45) {
        bq->active = FALSE;
        return;
    }
    double A = pow(10.0, gain_db / 40.0);
    double w0 = 2.0 * 3.14159265358979323846 * f0 / fs;
    double alpha = sin(w0) / (2.0 * 1.4);
    double cw = cos(w0);
    double a0 = 1.0 + alpha / A;
    bq->b0 = (1.0 + alpha * A) / a0;
    bq->b1 = (-2.0 * cw) / a0;
    bq->b2 = (1.0 - alpha * A) / a0;
    bq->a1 = (-2.0 * cw) / a0;
    bq->a2 = (1.0 - alpha / A) / a0;
    bq->active = TRUE;
}

static inline double vibez_biquad_run(VibezBiquad* bq, double x, int ch) {
    double y = bq->b0 * x + bq->z1[ch];
    bq->z1[ch] = bq->b1 * x - bq->a1 * y + bq->z2[ch];
    bq->z2[ch] = bq->b2 * x - bq->a2 * y;
    return y;
}

typedef struct {
    GMutex mu;
    gchar* next_uri;
    gboolean trim_next; // the next stream to start is a whole track
    gboolean trim_cur;  // the current stream's first segment still needs trimming
    guint64 trimmed;    // time cut from earlier streams since the last flush
    gdouble eq_gains[VIBEZ_EQ_BANDS];
    guint32 eq_ver;
    guint32 eq_seen;
    gint eq_rate;
    VibezBiquad eq_bank[VIBEZ_EQ_BANDS];
} VibezGstState;

// ── Embedded GstElement: vibez_aac_parse ────────────────────────────────────
// Parses ADTS AAC frames into raw AAC frames with codec_data (AudioSpecificConfig)
// and TIME segments so decodebin3/playbin3 works without gst-plugins-good's aacparse.

typedef struct {
    GstElement parent;
    GstPad* sinkpad;
    GstPad* srcpad;
    GByteArray* adapter;
    GstEvent* pending_segment;
    gint rate;
    gint channels;
    gboolean caps_set;
    gboolean synced;
    guint8 hdr_proto[4];
    guint64 frames_pushed;
} VibezAacParse;

typedef struct {
    GstElementClass parent_class;
} VibezAacParseClass;

static GType vibez_aac_parse_get_type(void);
G_DEFINE_TYPE(VibezAacParse, vibez_aac_parse, GST_TYPE_ELEMENT)

static GstStaticPadTemplate vibez_aac_sink_tmpl = GST_STATIC_PAD_TEMPLATE(
    "sink",
    GST_PAD_SINK,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS("audio/mpeg, mpegversion=(int){ 2, 4 }")
);

static GstStaticPadTemplate vibez_aac_src_tmpl = GST_STATIC_PAD_TEMPLATE(
    "src",
    GST_PAD_SRC,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS("audio/mpeg, framed=(boolean)true, mpegversion=(int){ 2, 4 }, stream-format=(string){ raw, adts }")
);

static const int vibez_adts_rates[16] = {
    96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
    16000, 12000, 11025, 8000, 7350, 0, 0, 0
};

static GstFlowReturn vibez_aac_parse_chain(GstPad* pad, GstObject* parent, GstBuffer* buf) {
    VibezAacParse* self = (VibezAacParse*)parent;
    GstMapInfo map;
    if (gst_buffer_map(buf, &map, GST_MAP_READ)) {
        g_byte_array_append(self->adapter, map.data, map.size);
        gst_buffer_unmap(buf, &map);
    }
    gst_buffer_unref(buf);

    GstFlowReturn ret = GST_FLOW_OK;
    guint offset = 0;
    guint len = self->adapter->len;
    const guint8* data = self->adapter->data;

    while (offset + 7 <= len) {
        if (data[offset] != 0xFF || (data[offset + 1] & 0xF6) != 0xF0) {
            self->synced = FALSE;
            offset++;
            continue;
        }
        guint hdr_len = (data[offset + 1] & 0x01) ? 7 : 9;
        guint frame_len = ((guint)(data[offset + 3] & 0x03) << 11) |
                          ((guint)data[offset + 4] << 3) |
                          ((guint)(data[offset + 5] >> 5) & 0x07);
        int freq_idx = (data[offset + 2] >> 2) & 0x0F;
        int chan_cfg = ((data[offset + 2] & 0x01) << 2) | ((data[offset + 3] >> 6) & 0x03);
        int rate = vibez_adts_rates[freq_idx];
        if (frame_len <= hdr_len || rate <= 0 || chan_cfg == 0) {
            self->synced = FALSE;
            offset++;
            continue;
        }
        if (self->synced &&
            (data[offset + 1] != self->hdr_proto[1] ||
             data[offset + 2] != self->hdr_proto[2] ||
             (data[offset + 3] & 0xFC) != (self->hdr_proto[3] & 0xFC))) {
            self->synced = FALSE;
            offset++;
            continue;
        }
        if (offset + frame_len > len) {
            break;
        }
        // When unsynced, verify that the byte immediately after this candidate
        // frame is another valid ADTS header with matching stream parameters.
        if (!self->synced) {
            if (offset + frame_len + 7 <= len) {
                const guint8* next = data + offset + frame_len;
                if (next[0] != 0xFF ||
                    next[1] != data[offset + 1] ||
                    next[2] != data[offset + 2] ||
                    (next[3] & 0xFC) != (data[offset + 3] & 0xFC)) {
                    offset++;
                    continue;
                }
            } else if (offset > 0) {
                // Wait for more bytes to confirm mid-buffer resync candidate.
                break;
            }
            self->synced = TRUE;
            memcpy(self->hdr_proto, data + offset, 4);
        }

        int profile = ((data[offset + 2] >> 6) & 0x03) + 1;
        int channels = (chan_cfg == 7) ? 8 : chan_cfg;
        if (!self->caps_set || self->rate != rate || self->channels != channels) {
            self->rate = rate;
            self->channels = channels;
            self->caps_set = TRUE;

            guint8 asc[2];
            asc[0] = (guint8)((profile << 3) | ((freq_idx >> 1) & 0x07));
            asc[1] = (guint8)(((freq_idx & 0x01) << 7) | (chan_cfg << 3));
            GstBuffer* codec_buf = gst_buffer_new_allocate(NULL, 2, NULL);
            gst_buffer_fill(codec_buf, 0, asc, 2);

            GstCaps* caps = gst_caps_new_simple("audio/mpeg",
                "mpegversion", G_TYPE_INT, 4,
                "framed", G_TYPE_BOOLEAN, TRUE,
                "stream-format", G_TYPE_STRING, "raw",
                "rate", G_TYPE_INT, rate,
                "channels", G_TYPE_INT, channels,
                "codec_data", GST_TYPE_BUFFER, codec_buf,
                NULL);
            gst_buffer_unref(codec_buf);
            gst_pad_set_caps(self->srcpad, caps);
            gst_caps_unref(caps);

            if (self->pending_segment) {
                gst_pad_push_event(self->srcpad, self->pending_segment);
                self->pending_segment = NULL;
            } else {
                GstSegment seg;
                gst_segment_init(&seg, GST_FORMAT_TIME);
                gst_pad_push_event(self->srcpad, gst_event_new_segment(&seg));
            }
        }

        guint raw_len = frame_len - hdr_len;
        GstBuffer* out = gst_buffer_new_allocate(NULL, raw_len, NULL);
        gst_buffer_fill(out, 0, data + offset + hdr_len, raw_len);
        GstClockTime pts = gst_util_uint64_scale_int(self->frames_pushed * 1024, GST_SECOND, rate);
        GstClockTime dur = gst_util_uint64_scale_int(1024, GST_SECOND, rate);
        GST_BUFFER_PTS(out) = pts;
        GST_BUFFER_DTS(out) = pts;
        GST_BUFFER_DURATION(out) = dur;
        self->frames_pushed++;

        offset += frame_len;
        ret = gst_pad_push(self->srcpad, out);
        if (ret != GST_FLOW_OK) {
            break;
        }
    }

    if (offset > 0) {
        g_byte_array_remove_range(self->adapter, 0, offset);
    }
    return ret;
}

static gboolean vibez_aac_parse_sink_event(GstPad* pad, GstObject* parent, GstEvent* event) {
    VibezAacParse* self = (VibezAacParse*)parent;
    switch (GST_EVENT_TYPE(event)) {
    case GST_EVENT_STREAM_START:
        g_byte_array_set_size(self->adapter, 0);
        if (self->pending_segment) {
            gst_event_unref(self->pending_segment);
            self->pending_segment = NULL;
        }
        self->synced = FALSE;
        self->frames_pushed = 0;
        return gst_pad_event_default(pad, parent, event);
    case GST_EVENT_CAPS:
        gst_event_unref(event);
        return TRUE;
    case GST_EVENT_SEGMENT: {
        self->frames_pushed = 0;
        GstSegment seg;
        gst_segment_init(&seg, GST_FORMAT_TIME);
        GstEvent* tev = gst_event_new_segment(&seg);
        gst_event_set_seqnum(tev, gst_event_get_seqnum(event));
        gst_event_unref(event);
        if (self->caps_set) {
            return gst_pad_push_event(self->srcpad, tev);
        }
        if (self->pending_segment) gst_event_unref(self->pending_segment);
        self->pending_segment = tev;
        return TRUE;
    }
    case GST_EVENT_FLUSH_STOP:
        g_byte_array_set_size(self->adapter, 0);
        if (self->pending_segment) {
            gst_event_unref(self->pending_segment);
            self->pending_segment = NULL;
        }
        self->synced = FALSE;
        self->frames_pushed = 0;
        return gst_pad_event_default(pad, parent, event);
    case GST_EVENT_EOS:
        g_byte_array_set_size(self->adapter, 0);
        self->synced = FALSE;
        return gst_pad_event_default(pad, parent, event);
    default:
        return gst_pad_event_default(pad, parent, event);
    }
}

static GstStateChangeReturn vibez_aac_parse_change_state(GstElement* element, GstStateChange transition) {
    VibezAacParse* self = (VibezAacParse*)element;
    if (transition == GST_STATE_CHANGE_PAUSED_TO_READY) {
        g_byte_array_set_size(self->adapter, 0);
        if (self->pending_segment) {
            gst_event_unref(self->pending_segment);
            self->pending_segment = NULL;
        }
        self->caps_set = FALSE;
        self->synced = FALSE;
        self->frames_pushed = 0;
    }
    return GST_ELEMENT_CLASS(vibez_aac_parse_parent_class)->change_state(element, transition);
}

static void vibez_aac_parse_finalize(GObject* object) {
    VibezAacParse* self = (VibezAacParse*)object;
    if (self->pending_segment) {
        gst_event_unref(self->pending_segment);
        self->pending_segment = NULL;
    }
    if (self->adapter) {
        g_byte_array_free(self->adapter, TRUE);
        self->adapter = NULL;
    }
    G_OBJECT_CLASS(vibez_aac_parse_parent_class)->finalize(object);
}

static void vibez_aac_parse_class_init(VibezAacParseClass* klass) {
    GObjectClass* gobject_class = G_OBJECT_CLASS(klass);
    GstElementClass* element_class = GST_ELEMENT_CLASS(klass);
    gobject_class->finalize = vibez_aac_parse_finalize;
    element_class->change_state = vibez_aac_parse_change_state;
    gst_element_class_add_static_pad_template(element_class, &vibez_aac_sink_tmpl);
    gst_element_class_add_static_pad_template(element_class, &vibez_aac_src_tmpl);
    gst_element_class_set_static_metadata(element_class,
        "Vibez AAC Parser", "Codec/Parser/Audio",
        "Embedded ADTS AAC frame parser", "vibez");
}

static void vibez_aac_parse_init(VibezAacParse* self) {
    self->adapter = g_byte_array_new();
    self->sinkpad = gst_pad_new_from_static_template(&vibez_aac_sink_tmpl, "sink");
    gst_pad_set_chain_function(self->sinkpad, vibez_aac_parse_chain);
    gst_pad_set_event_function(self->sinkpad, vibez_aac_parse_sink_event);
    gst_element_add_pad(GST_ELEMENT(self), self->sinkpad);

    self->srcpad = gst_pad_new_from_static_template(&vibez_aac_src_tmpl, "src");
    gst_pad_use_fixed_caps(self->srcpad);
    gst_element_add_pad(GST_ELEMENT(self), self->srcpad);
}

// ── Embedded GstElement: vibez_http_src ─────────────────────────────────────
// Connects to the local loopback HTTP stream server and streams the response
// body through coreelements' fdsrc, removing any dependency on souphttpsrc.

typedef struct {
    GstBin parent;
    GstElement* fdsrc;
    gchar* uri;
    int sock_fd;
} VibezHttpSrc;

typedef struct {
    GstBinClass parent_class;
} VibezHttpSrcClass;

static void vibez_http_src_uri_handler_init(gpointer g_iface, gpointer iface_data);
static GType vibez_http_src_get_type(void);

G_DEFINE_TYPE_WITH_CODE(VibezHttpSrc, vibez_http_src, GST_TYPE_BIN,
    G_IMPLEMENT_INTERFACE(GST_TYPE_URI_HANDLER, vibez_http_src_uri_handler_init))

static GstStaticPadTemplate vibez_http_src_tmpl = GST_STATIC_PAD_TEMPLATE(
    "src",
    GST_PAD_SRC,
    GST_PAD_ALWAYS,
    GST_STATIC_CAPS_ANY
);

static GstURIType vibez_http_src_uri_get_type(GType type) {
    return GST_URI_SRC;
}

static const gchar* const* vibez_http_src_uri_get_protocols(GType type) {
    static const gchar* protocols[] = {"http", NULL};
    return protocols;
}

static gchar* vibez_http_src_uri_get_uri(GstURIHandler* handler) {
    VibezHttpSrc* self = (VibezHttpSrc*)handler;
    GST_OBJECT_LOCK(self);
    gchar* r = g_strdup(self->uri);
    GST_OBJECT_UNLOCK(self);
    return r;
}

static gboolean vibez_http_src_uri_set_uri(GstURIHandler* handler, const gchar* uri, GError** error) {
    VibezHttpSrc* self = (VibezHttpSrc*)handler;
    GST_OBJECT_LOCK(self);
    g_free(self->uri);
    self->uri = g_strdup(uri);
    GST_OBJECT_UNLOCK(self);
    return TRUE;
}

static void vibez_http_src_uri_handler_init(gpointer g_iface, gpointer iface_data) {
    GstURIHandlerInterface* iface = (GstURIHandlerInterface*)g_iface;
    iface->get_type = vibez_http_src_uri_get_type;
    iface->get_protocols = vibez_http_src_uri_get_protocols;
    iface->get_uri = vibez_http_src_uri_get_uri;
    iface->set_uri = vibez_http_src_uri_set_uri;
}

enum {
    VIBEZ_HTTP_PROP_0,
    VIBEZ_HTTP_PROP_URI
};

static int vibez_connect_http_stream(const char* uri) {
    if (!uri) return -1;
    const char* p = uri;
    if (strncmp(p, "http://", 7) == 0) p += 7;
    const char* slash = strchr(p, '/');
    const char* path = slash ? slash : "/";
    char hostport[128] = {0};
    size_t hp_len = slash ? (size_t)(slash - p) : strlen(p);
    if (hp_len >= sizeof(hostport)) return -1;
    memcpy(hostport, p, hp_len);

    char host[64] = "127.0.0.1";
    int port = 80;
    char* colon = strchr(hostport, ':');
    if (colon) {
        *colon = '\0';
        snprintf(host, sizeof(host), "%s", hostport);
        port = atoi(colon + 1);
    } else if (hostport[0] != '\0') {
        snprintf(host, sizeof(host), "%s", hostport);
    }
    if (strcmp(host, "localhost") == 0) {
        snprintf(host, sizeof(host), "127.0.0.1");
    }

    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) return -1;

    struct sockaddr_in sin;
    memset(&sin, 0, sizeof(sin));
    sin.sin_family = AF_INET;
    sin.sin_port = htons((uint16_t)port);
    if (inet_pton(AF_INET, host, &sin.sin_addr) <= 0) {
        close(fd);
        return -1;
    }
    if (connect(fd, (struct sockaddr*)&sin, sizeof(sin)) < 0) {
        close(fd);
        return -1;
    }

    gchar* req = g_strdup_printf(
        "GET %s HTTP/1.0\r\nHost: 127.0.0.1:%d\r\nUser-Agent: vibez/2.0\r\nConnection: close\r\n\r\n",
        path, port);
    ssize_t sent = send(fd, req, strlen(req), 0);
    g_free(req);
    if (sent < 0) {
        close(fd);
        return -1;
    }

    char hdr[4096];
    size_t hlen = 0;
    while (hlen < sizeof(hdr) - 1) {
        ssize_t n = recv(fd, &hdr[hlen], 1, 0);
        if (n <= 0) {
            close(fd);
            return -1;
        }
        hlen++;
        if (hlen >= 4 && memcmp(&hdr[hlen - 4], "\r\n\r\n", 4) == 0) {
            hdr[hlen] = '\0';
            if (strstr(hdr, " 200 ") == NULL) {
                close(fd);
                return -1;
            }
            return fd;
        }
    }
    close(fd);
    return -1;
}

static GstStateChangeReturn vibez_http_src_change_state(GstElement* element, GstStateChange transition) {
    VibezHttpSrc* self = (VibezHttpSrc*)element;
    if (transition == GST_STATE_CHANGE_READY_TO_PAUSED) {
        GST_OBJECT_LOCK(self);
        gchar* u = g_strdup(self->uri);
        GST_OBJECT_UNLOCK(self);

        int fd = vibez_connect_http_stream(u);
        g_free(u);
        if (fd < 0) {
            GST_ELEMENT_ERROR(self, RESOURCE, OPEN_READ, ("Failed to connect to local audio stream"), (NULL));
            return GST_STATE_CHANGE_FAILURE;
        }
        self->sock_fd = fd;
        g_object_set(G_OBJECT(self->fdsrc), "fd", fd, NULL);
    }

    GstStateChangeReturn ret = GST_ELEMENT_CLASS(vibez_http_src_parent_class)->change_state(element, transition);

    if (transition == GST_STATE_CHANGE_PAUSED_TO_READY) {
        if (self->sock_fd >= 0) {
            close(self->sock_fd);
            self->sock_fd = -1;
        }
    }
    return ret;
}

static void vibez_http_src_set_property(GObject* object, guint prop_id, const GValue* value, GParamSpec* pspec) {
    VibezHttpSrc* self = (VibezHttpSrc*)object;
    if (prop_id == VIBEZ_HTTP_PROP_URI) {
        vibez_http_src_uri_set_uri((GstURIHandler*)self, g_value_get_string(value), NULL);
    } else {
        G_OBJECT_WARN_INVALID_PROPERTY_ID(object, prop_id, pspec);
    }
}

static void vibez_http_src_get_property(GObject* object, guint prop_id, GValue* value, GParamSpec* pspec) {
    VibezHttpSrc* self = (VibezHttpSrc*)object;
    if (prop_id == VIBEZ_HTTP_PROP_URI) {
        g_value_take_string(value, vibez_http_src_uri_get_uri((GstURIHandler*)self));
    } else {
        G_OBJECT_WARN_INVALID_PROPERTY_ID(object, prop_id, pspec);
    }
}

static void vibez_http_src_finalize(GObject* object) {
    VibezHttpSrc* self = (VibezHttpSrc*)object;
    if (self->sock_fd >= 0) {
        close(self->sock_fd);
        self->sock_fd = -1;
    }
    g_free(self->uri);
    G_OBJECT_CLASS(vibez_http_src_parent_class)->finalize(object);
}

static void vibez_http_src_class_init(VibezHttpSrcClass* klass) {
    GObjectClass* gobject_class = G_OBJECT_CLASS(klass);
    GstElementClass* element_class = GST_ELEMENT_CLASS(klass);
    gobject_class->set_property = vibez_http_src_set_property;
    gobject_class->get_property = vibez_http_src_get_property;
    gobject_class->finalize = vibez_http_src_finalize;
    element_class->change_state = vibez_http_src_change_state;
    g_object_class_install_property(gobject_class, VIBEZ_HTTP_PROP_URI,
        g_param_spec_string("uri", "URI", "URI", NULL, G_PARAM_READWRITE | G_PARAM_STATIC_STRINGS));
    gst_element_class_add_static_pad_template(element_class, &vibez_http_src_tmpl);
    gst_element_class_set_static_metadata(element_class,
        "Vibez Loopback HTTP Source", "Source/Network",
        "Embedded HTTP client source", "vibez");
}

static void vibez_http_src_init(VibezHttpSrc* self) {
    self->sock_fd = -1;
    self->fdsrc = gst_element_factory_make("fdsrc", "inner-fdsrc");
    gst_bin_add(GST_BIN(self), self->fdsrc);
    GstPad* target = gst_element_get_static_pad(self->fdsrc, "src");
    GstPad* ghost = gst_ghost_pad_new("src", target);
    gst_element_add_pad(GST_ELEMENT(self), ghost);
    gst_object_unref(target);
}
// AAC encoder delay: iTunes-encoded AAC starts with 2112 priming samples
// that ADTS has no field to describe, so the decoder plays them and every
// gapless join carries ~48 ms of near-silence. HE-AAC decoders add SBR delay
// on top; that part varies by decoder and is left alone.
#define VIBEZ_AAC_PRIMING 2112

// A start_seg stream begins mid-track (a seek), past the priming.
static gboolean vibez_uri_is_whole_track(const gchar* uri) {
    return uri && !strstr(uri, "start_seg=");
}

// on_filter_event moves each whole-track stream's segment start past the
// priming, so the sink drops those samples. Runs on the streaming thread.
static GstPadProbeReturn on_filter_event(GstPad* pad, GstPadProbeInfo* info, gpointer user_data) {
    VibezGstState* st = (VibezGstState*)user_data;
    GstEvent* ev = GST_PAD_PROBE_INFO_EVENT(info);
    if (GST_EVENT_TYPE(ev) == GST_EVENT_STREAM_START) {
        g_mutex_lock(&st->mu);
        st->trim_cur = st->trim_next;
        st->trim_next = FALSE;
        g_mutex_unlock(&st->mu);
        return GST_PAD_PROBE_OK;
    }
    if (GST_EVENT_TYPE(ev) != GST_EVENT_SEGMENT)
        return GST_PAD_PROBE_OK;

    const GstSegment* seg = NULL;
    gst_event_parse_segment(ev, &seg);
    // A base of zero is a fresh start (new URI or flush), even for a stream
    // that is not trimmed, such as a seek's start_seg stream.
    if (seg->base == 0) st->trimmed = 0;
    if (!st->trim_cur)
        return GST_PAD_PROBE_OK;
    gint rate = 0;
    GstCaps* caps = gst_pad_get_current_caps(pad);
    if (caps) {
        gst_structure_get_int(gst_caps_get_structure(caps, 0), "rate", &rate);
        gst_caps_unref(caps);
    }
    if (seg->format != GST_FORMAT_TIME || rate <= 0)
        return GST_PAD_PROBE_OK;
    st->trim_cur = FALSE;

    GstSegment s2;
    gst_segment_copy_into(seg, &s2);
    guint64 delay = gst_util_uint64_scale_int(VIBEZ_AAC_PRIMING, GST_SECOND, rate);
    // Upstream places each gapless stream at the untrimmed end of the
    // previous one, so pull it back by everything cut so far.
    s2.base -= MIN(s2.base, st->trimmed);
    st->trimmed += delay;
    s2.start += delay;
    if (s2.position < s2.start) s2.position = s2.start;
    GstEvent* nev = gst_event_new_segment(&s2);
    gst_event_set_seqnum(nev, gst_event_get_seqnum(ev));
    gst_event_unref(ev);
    GST_PAD_PROBE_INFO_DATA(info) = nev;
    return GST_PAD_PROBE_OK;
}

static void on_about_to_finish(GstElement* playbin, gpointer user_data) {
    VibezGstState* st = (VibezGstState*)user_data;
    if (!st) return;
    gchar* uri = NULL;
    g_mutex_lock(&st->mu);
    if (st->next_uri && st->next_uri[0] != '\0') {
        uri = st->next_uri;
        st->next_uri = NULL;
    }
    if (uri) st->trim_next = vibez_uri_is_whole_track(uri);
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
    // Register embedded fallback elements with rank MARGINAL + 1 so system
    // plugins (if installed) are used first, and our embedded implementations
    // seamlessly take over when gst-plugins-good (souphttpsrc / aacparse) is absent.
    gst_element_register(NULL, "vibezaacparse", GST_RANK_MARGINAL + 1, vibez_aac_parse_get_type());
    gst_element_register(NULL, "vibezhttpsrc", GST_RANK_MARGINAL + 1, vibez_http_src_get_type());
}

static int vibez_gst_has_element(const char* name) {
    GstElementFactory* f = gst_element_factory_find(name);
    if (f) {
        gst_object_unref(f);
        return 1;
    }
    return 0;
}

static void vibez_gst_set_element_rank(const char* name, int rank) {
    GstElementFactory* f = gst_element_factory_find(name);
    if (f) {
        gst_plugin_feature_set_rank(GST_PLUGIN_FEATURE(f), (guint)rank);
        gst_object_unref(f);
    }
}

static int vibez_gst_has_aac_decoder(void) {
    const char* candidates[] = {"avdec_aac", "faad", "fdkaacdec", NULL};
    for (int i = 0; candidates[i] != NULL; i++) {
        if (vibez_gst_has_element(candidates[i])) {
            return 1;
        }
    }
    return 0;
}

// on_filter_buffer applies the built-in 10-band biquad equalizer when
// gstreamer1.0-plugins-good's equalizer-10bands is not installed.
static GstPadProbeReturn on_filter_buffer(GstPad* pad, GstPadProbeInfo* info, gpointer user_data) {
    VibezGstState* st = (VibezGstState*)user_data;
    GstBuffer* buf = GST_PAD_PROBE_INFO_BUFFER(info);
    if (!buf) return GST_PAD_PROBE_OK;

    gint rate = 0, channels = 0;
    const gchar* fmt = NULL;
    GstCaps* caps = gst_pad_get_current_caps(pad);
    if (caps) {
        GstStructure* s = gst_caps_get_structure(caps, 0);
        gst_structure_get_int(s, "rate", &rate);
        gst_structure_get_int(s, "channels", &channels);
        fmt = gst_structure_get_string(s, "format");
        gst_caps_unref(caps);
    }
    if (rate <= 0 || channels <= 0 || channels > 2 || !fmt)
        return GST_PAD_PROBE_OK;

    g_mutex_lock(&st->mu);
    if (st->eq_ver != st->eq_seen || st->eq_rate != rate) {
        for (int b = 0; b < VIBEZ_EQ_BANDS; b++) {
            vibez_biquad_design(&st->eq_bank[b], (double)rate, vibez_eq_freqs[b], st->eq_gains[b]);
        }
        st->eq_seen = st->eq_ver;
        st->eq_rate = rate;
    }
    gboolean any_active = FALSE;
    for (int b = 0; b < VIBEZ_EQ_BANDS; b++) {
        if (st->eq_bank[b].active) {
            any_active = TRUE;
            break;
        }
    }
    if (!any_active) {
        g_mutex_unlock(&st->mu);
        return GST_PAD_PROBE_OK;
    }

    buf = gst_buffer_make_writable(buf);
    GST_PAD_PROBE_INFO_DATA(info) = buf;
    GstMapInfo map;
    if (gst_buffer_map(buf, &map, GST_MAP_READWRITE)) {
        if (strcmp(fmt, "S16LE") == 0) {
            gint16* samples = (gint16*)map.data;
            size_t frames = map.size / (sizeof(gint16) * (size_t)channels);
            for (size_t f = 0; f < frames; f++) {
                for (int c = 0; c < channels; c++) {
                    double x = (double)samples[f * (size_t)channels + (size_t)c];
                    for (int b = 0; b < VIBEZ_EQ_BANDS; b++) {
                        if (st->eq_bank[b].active) x = vibez_biquad_run(&st->eq_bank[b], x, c);
                    }
                    if (x > 32767.0) x = 32767.0;
                    if (x < -32768.0) x = -32768.0;
                    samples[f * (size_t)channels + (size_t)c] = (gint16)x;
                }
            }
        } else if (strcmp(fmt, "F32LE") == 0) {
            gfloat* samples = (gfloat*)map.data;
            size_t frames = map.size / (sizeof(gfloat) * (size_t)channels);
            for (size_t f = 0; f < frames; f++) {
                for (int c = 0; c < channels; c++) {
                    double x = (double)samples[f * (size_t)channels + (size_t)c];
                    for (int b = 0; b < VIBEZ_EQ_BANDS; b++) {
                        if (st->eq_bank[b].active) x = vibez_biquad_run(&st->eq_bank[b], x, c);
                    }
                    if (x > 1.0) x = 1.0;
                    if (x < -1.0) x = -1.0;
                    samples[f * (size_t)channels + (size_t)c] = (gfloat)x;
                }
            }
        }
        gst_buffer_unmap(buf, &map);
    }
    g_mutex_unlock(&st->mu);
    return GST_PAD_PROBE_OK;
}

// vibez_gst_new creates a playbin3 pipeline, falling back to playbin, and
// attaches a 10-band equalizer audio-filter when gstreamer1.0-plugins-good is
// installed (an identity filter with embedded biquad DSP otherwise, to carry
// both the priming trim and the 10-band EQ).
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
        } else if (!vibez_gst_has_element("autoaudiosink")) {
            // autoaudiosink ships in gst-plugins-good (libgstautodetect). When
            // it is absent, explicitly select pipewiresink, pulsesink, or
            // alsasink (which ships in gst-plugins-base).
            const char* sinks[] = {"pipewiresink", "pulsesink", "alsasink", NULL};
            for (int i = 0; sinks[i] != NULL; i++) {
                GstElement* sink = gst_element_factory_make(sinks[i], "vibez-sink");
                if (sink) {
                    g_object_set(G_OBJECT(p), "audio-sink", sink, NULL);
                    break;
                }
            }
        }
        GstElement* filter = NULL;
        if (eqOut) {
            filter = gst_element_factory_make("equalizer-10bands", "vibez-eq");
            *eqOut = filter;
        }
        // When equalizer-10bands is unavailable, identity carries both the
        // priming trim event probe and the embedded 10-band biquad buffer probe.
        gboolean use_embedded_eq = (filter == NULL);
        if (!filter) filter = gst_element_factory_make("identity", "vibez-filter");
        if (filter) {
            g_object_set(G_OBJECT(p), "audio-filter", filter, NULL);
            GstPad* sp = gst_element_get_static_pad(filter, "sink");
            if (sp) {
                gst_pad_add_probe(sp, GST_PAD_PROBE_TYPE_EVENT_DOWNSTREAM, on_filter_event, st, NULL);
                if (use_embedded_eq) {
                    gst_pad_add_probe(sp, GST_PAD_PROBE_TYPE_BUFFER, on_filter_buffer, st, NULL);
                }
                gst_object_unref(sp);
            }
        }
    }
    return p;
}

static void vibez_gst_set_eq_band(GstElement* eq, VibezGstState* st, gint idx, gdouble gain) {
    if (idx < 0 || idx >= VIBEZ_EQ_BANDS) return;
    if (gain < -24.0) gain = -24.0;
    if (gain > 12.0) gain = 12.0;
    if (st) {
        g_mutex_lock(&st->mu);
        st->eq_gains[idx] = gain;
        st->eq_ver++;
        g_mutex_unlock(&st->mu);
    }
    if (eq) {
        char prop[8];
        g_snprintf(prop, sizeof(prop), "band%d", idx);
        g_object_set(G_OBJECT(eq), prop, gain, NULL);
    }
}

static void vibez_gst_set_uri(GstElement* p, VibezGstState* st, const char* uri) {
    // Use READY (not NULL) to release the current media while keeping the
    // pipeline and its PipeWire/PulseAudio connection alive. Going to NULL
    // would tear down the audio sink and create a new OS-level player entry
    // on every track change, causing dozens of stale entries in notification
    // centres that track MPRIS/PipeWire streams.
    gst_element_set_state(p, GST_STATE_READY);
    g_mutex_lock(&st->mu);
    st->trim_next = vibez_uri_is_whole_track(uri);
    g_mutex_unlock(&st->mu);
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
        gchar* dbg = NULL;
        gst_message_parse_error(msg, &e, &dbg);
        if (e) {
            if (dbg && dbg[0] != '\0') {
                *strOut = g_strdup_printf("%s [%s]", e->message, dbg);
            } else {
                *strOut = g_strdup(e->message);
            }
            g_error_free(e);
        }
        g_free(dbg);
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
// faad, or fdkaacdec) is installed for Apple Music AAC streaming. HTTP source
// handling and ADTS frame parsing have embedded fallbacks (vibezhttpsrc and
// vibezaacparse) so gstreamer1.0-plugins-good is not required.
func EnsureAACDecoder() error {
	once.Do(func() { C.vibez_gst_init() })
	if C.vibez_gst_has_aac_decoder() == 0 {
		return fmt.Errorf("gst: no GStreamer AAC decoder plugin found — %s", aacDecoderHelp)
	}
	return nil
}

func setElementRank(name string, rank int) {
	once.Do(func() { C.vibez_gst_init() })
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	C.vibez_gst_set_element_rank(cs, C.int(rank))
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
	C.vibez_gst_set_uri(p.pipeline, p.st, cs)
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
	for i := range 10 {
		gain := 0.0
		if i < len(gains) {
			gain = gains[i]
		}
		C.vibez_gst_set_eq_band(p.eq, p.st, C.gint(i), C.gdouble(gain))
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
