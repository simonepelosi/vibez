#ifndef VIBEZ_SINK_DARWIN_H
#define VIBEZ_SINK_DARWIN_H

#ifdef __cplusplus
extern "C" {
#endif

typedef void* vibez_avplayer_t;

typedef void (*vibez_eos_cb_t)(void* user_data);
typedef void (*vibez_err_cb_t)(void* user_data, const char* err_msg);

vibez_avplayer_t vibez_avplayer_create(void* user_data, vibez_eos_cb_t on_eos, vibez_err_cb_t on_err);
void vibez_avplayer_play_uri(vibez_avplayer_t handle, const char* uri);
void vibez_avplayer_play(vibez_avplayer_t handle);
void vibez_avplayer_pause(vibez_avplayer_t handle);
void vibez_avplayer_stop(vibez_avplayer_t handle);
void vibez_avplayer_seek(vibez_avplayer_t handle, double seconds);
void vibez_avplayer_set_volume(vibez_avplayer_t handle, float volume);
double vibez_avplayer_position(vibez_avplayer_t handle);
void vibez_avplayer_destroy(vibez_avplayer_t handle);

#ifdef __cplusplus
}
#endif

#endif // VIBEZ_SINK_DARWIN_H
