#ifndef VIBEZ_LOCAL_SINK_WINDOWS_H
#define VIBEZ_LOCAL_SINK_WINDOWS_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef void* vibez_localsink_t;

typedef void (*vibez_localsink_eos_cb_t)(uintptr_t user_data, uint64_t gen);
typedef void (*vibez_localsink_err_cb_t)(uintptr_t user_data, uint64_t gen, char* err_msg);

vibez_localsink_t vibez_localsink_create(uintptr_t user_data, vibez_localsink_eos_cb_t on_eos, vibez_localsink_err_cb_t on_err);
// vibez_localsink_load opens path_utf8, resolves its duration (out_duration_sec),
// and starts playback when autoplay != 0. On failure it writes an error message
// into err_buf and returns non-zero.
int vibez_localsink_load(vibez_localsink_t handle, const char* path_utf8, uint64_t gen, int autoplay, double* out_duration_sec, char* err_buf, int err_buf_len);
void vibez_localsink_play(vibez_localsink_t handle);
void vibez_localsink_pause(vibez_localsink_t handle);
void vibez_localsink_stop(vibez_localsink_t handle);
void vibez_localsink_seek(vibez_localsink_t handle, double seconds);
void vibez_localsink_set_volume(vibez_localsink_t handle, double volume);
void vibez_localsink_set_eq(vibez_localsink_t handle, const double* gains_db, int count);
void vibez_localsink_unload(vibez_localsink_t handle);
double vibez_localsink_position(vibez_localsink_t handle);
double vibez_localsink_duration(vibez_localsink_t handle);
int vibez_localsink_is_playing(vibez_localsink_t handle);
void vibez_localsink_destroy(vibez_localsink_t handle);

#ifdef __cplusplus
}
#endif

#endif // VIBEZ_LOCAL_SINK_WINDOWS_H
