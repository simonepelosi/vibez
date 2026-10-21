#ifndef VIBEZ_SINK_DARWIN_H
#define VIBEZ_SINK_DARWIN_H

#ifdef __cplusplus
extern "C" {
#endif

#include <stdint.h>

typedef void* vibez_darwinsink_t;

typedef void (*vibez_darwinsink_eos_cb_t)(uintptr_t user_data);
typedef void (*vibez_darwinsink_err_cb_t)(uintptr_t user_data, char* err_msg);

vibez_darwinsink_t vibez_darwinsink_create(uintptr_t user_data, vibez_darwinsink_eos_cb_t on_eos, vibez_darwinsink_err_cb_t on_err);
void vibez_darwinsink_play_uri(vibez_darwinsink_t handle, const char* uri);
void vibez_darwinsink_play(vibez_darwinsink_t handle);
void vibez_darwinsink_pause(vibez_darwinsink_t handle);
void vibez_darwinsink_stop(vibez_darwinsink_t handle);
void vibez_darwinsink_seek(vibez_darwinsink_t handle, double seconds);
void vibez_darwinsink_set_volume(vibez_darwinsink_t handle, float volume);
double vibez_darwinsink_position(vibez_darwinsink_t handle);
void vibez_darwinsink_destroy(vibez_darwinsink_t handle);

#ifdef __cplusplus
}
#endif

#endif // VIBEZ_SINK_DARWIN_H
