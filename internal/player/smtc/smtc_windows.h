#ifndef VIBEZ_SMTC_WINDOWS_H
#define VIBEZ_SMTC_WINDOWS_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef void* vibez_smtc_t;

// Events reported to Go. The values are this bridge's own, not WinRT's.
enum {
    VIBEZ_SMTC_EVENT_PLAY = 1,
    VIBEZ_SMTC_EVENT_PAUSE = 2,
    VIBEZ_SMTC_EVENT_STOP = 3,
    VIBEZ_SMTC_EVENT_NEXT = 4,
    VIBEZ_SMTC_EVENT_PREVIOUS = 5,
    VIBEZ_SMTC_EVENT_SEEK = 6, // position_hns holds the requested position
};

// Playback status values. They equal Windows.Media.MediaPlaybackStatus.
// CLOSED also disables the controls, so the session leaves the system UI.
enum {
    VIBEZ_SMTC_STATUS_CLOSED = 0,
    VIBEZ_SMTC_STATUS_CHANGING = 1,
    VIBEZ_SMTC_STATUS_STOPPED = 2,
    VIBEZ_SMTC_STATUS_PLAYING = 3,
    VIBEZ_SMTC_STATUS_PAUSED = 4,
};

// Called on a Windows thread pool thread. It must not block.
typedef void (*vibez_smtc_event_cb_t)(uintptr_t user_data, int event, int64_t position_hns);
// Called on the bridge's own thread when applying an update fails.
typedef void (*vibez_smtc_error_cb_t)(uintptr_t user_data, char* msg);

// One batch of changes. A field group is applied only when its has_ flag is
// set. Strings are UTF-8 and are copied before vibez_smtc_update returns.
typedef struct {
    int has_metadata;
    const char* title;
    const char* artist;
    const char* album;
    const char* artwork_url; // http(s) URL or empty for no thumbnail
    int has_status;
    int status; // VIBEZ_SMTC_STATUS_*
    int has_timeline;
    int64_t position_hns; // 100 ns units, like Windows.Foundation.TimeSpan
    int64_t duration_hns;
} vibez_smtc_update_t;

// vibez_smtc_create starts the bridge thread, creates its hidden window and
// obtains the window's SystemMediaTransportControls. It returns NULL on
// failure and stores the failing HRESULT and a description of the failed step
// (a static string) in hr_out and stage_out.
vibez_smtc_t vibez_smtc_create(uintptr_t user_data, vibez_smtc_event_cb_t on_event, vibez_smtc_error_cb_t on_error,
                               int32_t* hr_out, const char** stage_out);

// vibez_smtc_update queues u for the bridge thread and returns at once.
// Batches queued before the thread runs are merged, newest field group wins.
void vibez_smtc_update(vibez_smtc_t handle, const vibez_smtc_update_t* u);

// vibez_smtc_destroy disables the controls, unregisters the event handlers,
// destroys the window and joins the bridge thread. No event callback starts
// after it returns.
void vibez_smtc_destroy(vibez_smtc_t handle);

#ifdef __cplusplus
}
#endif

#endif // VIBEZ_SMTC_WINDOWS_H
