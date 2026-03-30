package webkit

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.0

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

// vibez_hide_window makes the GTK window completely invisible:
//   - hidden before the first GTK paint
//   - excluded from taskbar and pager
//   - zero opacity fallback
//   - off-screen position
//   - UTILITY hint so window managers ignore it
static void vibez_hide_window(void* ptr) {
    GtkWidget* w = GTK_WIDGET(ptr);
    GtkWindow* win = GTK_WINDOW(ptr);

    gtk_widget_hide(w);
    gtk_window_set_skip_taskbar_hint(win, TRUE);
    gtk_window_set_skip_pager_hint(win, TRUE);
    gtk_window_set_decorated(win, FALSE);
    gtk_window_set_type_hint(win, GDK_WINDOW_TYPE_HINT_UTILITY);
    gtk_widget_set_opacity(w, 0.0);
    gtk_window_move(win, -32000, -32000);
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
    gtk_window_move(win, 100, 100);
    gtk_window_set_default_size(win, 800, 600);
    gtk_widget_show_all(w);
    gtk_window_present(win);
}

// vibez_on_map_cb is connected to the GtkWindow "map" signal. It immediately
// re-hides the window if its opacity is 0. This prevents the brief flash that
// occurs because webview.New() calls gtk_widget_show_all() internally before
// we get a chance to hide the window.
static void vibez_on_map_cb(GtkWidget* w, gpointer data) {
    if (gtk_widget_get_opacity(w) < 0.1) {
        gtk_widget_hide(w);
        gtk_window_move(GTK_WINDOW(w), -32000, -32000);
    }
}

static void vibez_connect_hide_on_map(void* ptr) {
    g_signal_connect(GTK_WIDGET(ptr), "map", G_CALLBACK(vibez_on_map_cb), NULL);
}

// vibez_find_webview recursively searches the GTK widget tree for the
// first WebKitWebView child.
static WebKitWebView* vibez_find_webview(GtkWidget* w) {
    if (WEBKIT_IS_WEB_VIEW(w)) return WEBKIT_WEB_VIEW(w);
    if (!GTK_IS_CONTAINER(w)) return NULL;
    GList* children = gtk_container_get_children(GTK_CONTAINER(w));
    WebKitWebView* result = NULL;
    for (GList* l = children; l && !result; l = l->next)
        result = vibez_find_webview(GTK_WIDGET(l->data));
    g_list_free(children);
    return result;
}

// vibez_allow_autoplay disables WebKit's "user gesture required for media
// playback" restriction so MusicKit JS can call play() programmatically.
// It also disables hardware acceleration — vibez is a hidden audio-only
// player and has no need for GPU rendering. Without this, WebKit's GPU
// subprocess competes with the system compositor, causing brief system-wide
// stalls when the GPU command buffer is flushed.
static void vibez_allow_autoplay(void* win_ptr) {
    WebKitWebView* wv = vibez_find_webview(GTK_WIDGET(win_ptr));
    if (!wv) return;
    WebKitSettings* s = webkit_web_view_get_settings(wv);
    webkit_settings_set_media_playback_requires_user_gesture(s, FALSE);
    webkit_settings_set_hardware_acceleration_policy(
        s, WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER);
}
*/
import "C"
import "unsafe"

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
