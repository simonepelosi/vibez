package webkit

/*
#cgo pkg-config: gtk+-3.0

#include <gtk/gtk.h>

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
*/
import "C"
import "unsafe"

func hideGTKWindow(ptr unsafe.Pointer) {
	C.vibez_hide_window(ptr)
}

func showGTKWindow(ptr unsafe.Pointer) {
	C.vibez_show_window(ptr)
}
