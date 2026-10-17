#ifndef CDM_BRIDGE_H_
#define CDM_BRIDGE_H_

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct cdm_context cdm_context_t;

// cdm_context_create initializes the Widevine CDM library from the given path.
// Returns NULL on failure.
cdm_context_t* cdm_context_create(const char* library_path);

// cdm_context_set_server_certificate sets the Widevine server certificate.
// Returns 0 on success, negative error code on failure.
int cdm_context_set_server_certificate(cdm_context_t* ctx, const uint8_t* cert_data, uint32_t cert_size);

// cdm_context_generate_challenge creates a session for the given 16-byte Key ID (KID)
// and returns the binary Widevine license challenge.
// Caller must free out_challenge and out_session_id using free().
// Returns 0 on success.
int cdm_context_generate_challenge(
    cdm_context_t* ctx,
    const uint8_t* kid,
    uint32_t kid_size,
    uint8_t** out_challenge,
    uint32_t* out_challenge_size,
    char** out_session_id
);

// cdm_context_update_session updates the session with the license returned from Apple.
// Returns 0 on success (keys become usable).
int cdm_context_update_session(
    cdm_context_t* ctx,
    const char* session_id,
    const uint8_t* license_data,
    uint32_t license_size
);

// cdm_context_decrypt decrypts an audio sample buffer using AES-CTR (cenc).
// If num_subsamples > 0, clear_bytes and cipher_bytes specify the subsample pattern.
// out_decrypted must have at least in_size bytes allocated.
// Returns 0 on success.
int cdm_context_decrypt(
    cdm_context_t* ctx,
    const uint8_t* key_id,
    uint32_t key_id_size,
    const uint8_t* iv,
    uint32_t iv_size,
    const uint8_t* in_data,
    uint32_t in_size,
    const uint16_t* clear_bytes,
    const uint32_t* cipher_bytes,
    uint32_t num_subsamples,
    uint8_t* out_decrypted,
    uint32_t* out_size
);

// cdm_context_destroy destroys the CDM instance and unloads the shared library.
void cdm_context_destroy(cdm_context_t* ctx);

#ifdef __cplusplus
}
#endif

#endif // CDM_BRIDGE_H_
