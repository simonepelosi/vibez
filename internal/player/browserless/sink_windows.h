#ifndef VIBEZ_SINK_WINDOWS_H
#define VIBEZ_SINK_WINDOWS_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef void* vibez_winsink_t;

typedef void (*vibez_winsink_eos_cb_t)(uintptr_t user_data);
typedef void (*vibez_winsink_err_cb_t)(uintptr_t user_data, char* err_msg);
typedef void (*vibez_winsink_stream_start_cb_t)(uintptr_t user_data, char* uri_utf8);

// vibez_winsink_create returns a sink that decodes the ADTS AAC stream at a
// loopback URL with Media Foundation and plays the PCM through waveOut.
vibez_winsink_t vibez_winsink_create(
    uintptr_t user_data,
    vibez_winsink_eos_cb_t on_eos,
    vibez_winsink_err_cb_t on_err,
    vibez_winsink_stream_start_cb_t on_stream_start);
void vibez_winsink_play_uri(vibez_winsink_t handle, const char* uri_utf8);
void vibez_winsink_set_next_uri(vibez_winsink_t handle, const char* uri_utf8);
void vibez_winsink_play(vibez_winsink_t handle);
void vibez_winsink_pause(vibez_winsink_t handle);
void vibez_winsink_stop(vibez_winsink_t handle);
void vibez_winsink_set_volume(vibez_winsink_t handle, double volume);
// vibez_winsink_set_eq sets gains in dB for the 10 bands, lowest first.
void vibez_winsink_set_eq(vibez_winsink_t handle, const double* gains_db, int count);
// vibez_winsink_position returns seconds of audio played since the last play_uri.
double vibez_winsink_position(vibez_winsink_t handle);
void vibez_winsink_destroy(vibez_winsink_t handle);

#ifdef __cplusplus
}
#endif

#endif // VIBEZ_SINK_WINDOWS_H
