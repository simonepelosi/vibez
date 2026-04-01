package webkit

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>

// vibez_hide_window makes the GTK window completely invisible.
// Under Wayland we MUST NOT use gtk_window_move() — positioning a window
// off-screen is a Wayland protocol violation that triggers a compositor
// crash. Instead we rely solely on gtk_widget_hide() + zero opacity +
// UTILITY window hint, which are all valid Wayland operations.
static void vibez_hide_window(void* ptr) {
    GtkWidget* w = GTK_WIDGET(ptr);
    GtkWindow* win = GTK_WINDOW(ptr);

    gtk_window_set_skip_taskbar_hint(win, TRUE);
    gtk_window_set_skip_pager_hint(win, TRUE);
    gtk_window_set_decorated(win, FALSE);
    gtk_window_set_type_hint(win, GDK_WINDOW_TYPE_HINT_UTILITY);
    gtk_widget_set_opacity(w, 0.0);
    gtk_widget_hide(w);
}

// vibez_show_window makes the GTK window visible so the user can interact
// with MusicKit JS's authorization UI (e.g. sign-in sheet).
static void vibez_show_window(void* ptr) {
    GtkWidget* w = GTK_WIDGET(ptr);
    GtkWindow* win = GTK_WINDOW(ptr);

    gtk_window_set_skip_taskbar_hint(win, FALSE);
    gtk_window_set_skip_pager_hint(win, FALSE);
    gtk_window_set_decorated(win, TRUE);
    gtk_window_set_type_hint(win, GDK_WINDOW_TYPE_HINT_NORMAL);
    gtk_widget_set_opacity(w, 1.0);
    gtk_window_set_default_size(win, 800, 600);
    gtk_widget_show_all(w);
    gtk_window_present(win);
}

// vibez_on_map_cb is connected to the GtkWindow "map" signal. It immediately
// re-hides the window if its opacity is 0. This prevents the brief flash that
// occurs because webview.New() calls gtk_widget_show_all() internally.
// NOTE: do NOT call gtk_window_move() here — off-screen positioning is a
// Wayland protocol violation that crashes the compositor.
static void vibez_on_map_cb(GtkWidget* w, gpointer data) {
    if (gtk_widget_get_opacity(w) < 0.1) {
        gtk_widget_hide(w);
    }
}

static void vibez_connect_hide_on_map(void* ptr) {
    g_signal_connect(GTK_WIDGET(ptr), "map", G_CALLBACK(vibez_on_map_cb), NULL);
}

// vibez_find_webview recursively searches the GTK widget tree for the
// first WebKitWebView child.
static WebKitWebView* vibez_find_webview(GtkWidget* w) {
    if (!w) return NULL;
    if (WEBKIT_IS_WEB_VIEW(w)) return WEBKIT_WEB_VIEW(w);
    if (!GTK_IS_CONTAINER(w)) return NULL;
    GList* children = gtk_container_get_children(GTK_CONTAINER(w));
    WebKitWebView* result = NULL;
    for (GList* l = children; l && !result; l = l->next) {
        if (l->data)
            result = vibez_find_webview(GTK_WIDGET(l->data));
    }
    g_list_free(children);
    return result;
}

// vibez_load_html_localhost loads HTML content with "http://localhost/" as the
// base URI so the page gets a "potentially trustworthy" secure context.
// webview_go's SetHtml() uses a NULL base URI which gives the page a null
// (opaque) origin — not a secure context. EME (navigator.requestMediaKeySystemAccess)
// is only exposed in secure contexts, so without this it always throws TypeError.
static void vibez_load_html_localhost(void* win_ptr, const char* html) {
    if (!win_ptr || !html) return;
    WebKitWebView* wv = vibez_find_webview(GTK_WIDGET(win_ptr));
    if (!wv) return;
    webkit_web_view_load_html(wv, html, "http://localhost/");
}

// vibez_allow_autoplay disables WebKit's "user gesture required for media
// playback" restriction so MusicKit JS can call play() programmatically.
// We also enable EME and MSE flags so they are active if GStreamer CDM plugins
// are ever installed (the Ubuntu-packaged WebKit2GTK omits them at build time,
// so navigator.requestMediaKeySystemAccess is undefined and FairPlay DRM is
// unavailable; MusicKit falls back to 30-second unencrypted previews).
// Hardware acceleration is ON_DEMAND so the GPU pipeline is available for any
// media decoding; compositing mode is disabled separately via env var to avoid
// conflicts with the system compositor on track transitions.
// The WebView is muted because GStreamer is the sole audio output — MusicKit
// still controls queue/metadata/auth, but its audio element is silenced here.
static void vibez_allow_autoplay(void* win_ptr) {
    if (!win_ptr) return;
    WebKitWebView* wv = vibez_find_webview(GTK_WIDGET(win_ptr));
    if (!wv) return;
    WebKitSettings* s = webkit_web_view_get_settings(wv);
    if (!s) return;
    webkit_settings_set_media_playback_requires_user_gesture(s, FALSE);
    webkit_settings_set_enable_encrypted_media(s, TRUE);
    webkit_settings_set_enable_mediasource(s, TRUE);
    webkit_settings_set_hardware_acceleration_policy(
        s, WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND);
    // Mute the WebView — GStreamer provides the actual audio output.
    webkit_web_view_set_is_muted(wv, TRUE);
}
*/
import "C"
import (
	"unsafe"
)

func hideGTKWindow(ptr unsafe.Pointer) {
	C.vibez_hide_window(ptr)
}

func showGTKWindow(ptr unsafe.Pointer) {
	C.vibez_show_window(ptr)
}

// connectHideOnMap attaches a "map" signal handler that re-hides the window
// if its opacity is 0. Call this once after webview.New() to prevent the
// startup flash caused by webview internally calling gtk_widget_show_all().
func connectHideOnMap(ptr unsafe.Pointer) {
	C.vibez_connect_hide_on_map(ptr)
}

// allowAutoplay disables WebKit's autoplay restriction so MusicKit JS can
// call music.play() without a preceding user gesture.
func allowAutoplay(ptr unsafe.Pointer) {
	C.vibez_allow_autoplay(ptr)
}

// loadHTMLLocalhost loads HTML content with "http://localhost/" as the base URI
// so the page gets a secure context. This is required for EME
// (navigator.requestMediaKeySystemAccess) which is only available in secure
// contexts. webview_go's SetHtml uses a null base URI (opaque origin) which
// blocks EME with a TypeError even when the EME API is enabled in settings.
func loadHTMLLocalhost(ptr unsafe.Pointer, html string) {
	cs := C.CString(html)
	defer C.free(unsafe.Pointer(cs))
	C.vibez_load_html_localhost(ptr, cs)
}
