#ifndef LIBDAVE_C_H
#define LIBDAVE_C_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque handle types */
typedef struct dave_session_t dave_session_t;
typedef struct dave_encryptor_t dave_encryptor_t;
typedef struct dave_decryptor_t dave_decryptor_t;
typedef struct dave_key_ratchet_t dave_key_ratchet_t;

/* Media types matching discord::dave::MediaType */
typedef enum {
    DAVE_MEDIA_AUDIO = 0,
    DAVE_MEDIA_VIDEO = 1
} dave_media_type;

/* Encryptor result codes matching discord::dave::Encryptor::ResultCode */
typedef enum {
    DAVE_RESULT_SUCCESS = 0,
    DAVE_RESULT_UNINITIALIZED_CONTEXT = 1,
    DAVE_RESULT_INITIALIZATION_FAILURE = 2,
    DAVE_RESULT_UNSUPPORTED_CODEC = 3,
    DAVE_RESULT_ENCRYPTION_FAILURE = 4,
    DAVE_RESULT_FINALIZATION_FAILURE = 5,
    DAVE_RESULT_TAG_APPEND_FAILURE = 6
} dave_result_code;

/* Process commit result type */
typedef enum {
    DAVE_COMMIT_FAILED = 0,
    DAVE_COMMIT_IGNORED = 1,
    DAVE_COMMIT_OK = 2
} dave_commit_result_type;

/* ---- MLS Session ---- */

dave_session_t* dave_session_create(const char* auth_session_id);
void dave_session_destroy(dave_session_t* s);

void dave_session_init(dave_session_t* s,
                       uint16_t protocol_version,
                       uint64_t group_id,
                       const char* self_user_id);

void dave_session_reset(dave_session_t* s);

void dave_session_set_protocol_version(dave_session_t* s, uint16_t version);
uint16_t dave_session_get_protocol_version(dave_session_t* s);

/* Returns malloc'd buffer. Caller must dave_free() the result. */
uint8_t* dave_session_get_marshalled_key_package(dave_session_t* s,
                                                  size_t* out_len);

/* Process proposals. Returns malloc'd commit message or NULL.
 * recognized_user_ids is a null-terminated array of strings. */
uint8_t* dave_session_process_proposals(dave_session_t* s,
                                         const uint8_t* proposals,
                                         size_t proposals_len,
                                         const char** recognized_user_ids,
                                         size_t num_user_ids,
                                         size_t* out_len);

/* Process commit. Returns roster entries via out params.
 * result_type: 0=failed, 1=ignored, 2=ok
 * On ok, out_roster_user_ids and out_roster_keys are malloc'd arrays.
 * Caller must dave_free() each element and the arrays. */
void dave_session_process_commit(dave_session_t* s,
                                  const uint8_t* commit,
                                  size_t commit_len,
                                  int* result_type,
                                  uint64_t** out_roster_ids,
                                  uint8_t*** out_roster_keys,
                                  size_t** out_roster_key_lens,
                                  size_t* out_roster_count);

/* Process welcome. Returns roster via out params (same pattern as commit). */
int dave_session_process_welcome(dave_session_t* s,
                                  const uint8_t* welcome,
                                  size_t welcome_len,
                                  const char** recognized_user_ids,
                                  size_t num_user_ids,
                                  uint64_t** out_roster_ids,
                                  uint8_t*** out_roster_keys,
                                  size_t** out_roster_key_lens,
                                  size_t* out_roster_count);

/* Get key ratchet for a user. Returns ownership to caller. */
dave_key_ratchet_t* dave_session_get_key_ratchet(dave_session_t* s,
                                                   const char* user_id);

/* ---- Encryptor ---- */

dave_encryptor_t* dave_encryptor_create(void);
void dave_encryptor_destroy(dave_encryptor_t* e);

/* Set key ratchet. Takes ownership of kr. */
void dave_encryptor_set_key_ratchet(dave_encryptor_t* e,
                                     dave_key_ratchet_t* kr);

void dave_encryptor_set_passthrough_mode(dave_encryptor_t* e, int enabled);
int dave_encryptor_is_passthrough_mode(dave_encryptor_t* e);

size_t dave_encryptor_get_max_ciphertext_byte_size(dave_encryptor_t* e,
                                                     dave_media_type media_type,
                                                     size_t frame_size);

int dave_encryptor_encrypt(dave_encryptor_t* e,
                            dave_media_type media_type,
                            uint32_t ssrc,
                            const uint8_t* frame,
                            size_t frame_len,
                            uint8_t* encrypted_frame,
                            size_t encrypted_frame_capacity,
                            size_t* bytes_written);

/* ---- Decryptor ---- */

dave_decryptor_t* dave_decryptor_create(void);
void dave_decryptor_destroy(dave_decryptor_t* d);

size_t dave_decryptor_get_max_plaintext_byte_size(dave_decryptor_t* d,
                                                    dave_media_type media_type,
                                                    size_t encrypted_frame_size);

size_t dave_decryptor_decrypt(dave_decryptor_t* d,
                               dave_media_type media_type,
                               const uint8_t* encrypted_frame,
                               size_t encrypted_frame_len,
                               uint8_t* frame,
                               size_t frame_capacity);

/* Transition to a new key ratchet. Takes ownership of kr.
 * transition_expiry_ms: duration in ms (0 = use default 10s). */
void dave_decryptor_transition_to_key_ratchet(dave_decryptor_t* d,
                                                dave_key_ratchet_t* kr,
                                                uint32_t transition_expiry_ms);

void dave_decryptor_transition_to_passthrough_mode(dave_decryptor_t* d,
                                                     int passthrough,
                                                     uint32_t transition_expiry_ms);

/* ---- Key Ratchet ---- */

void dave_key_ratchet_destroy(dave_key_ratchet_t* kr);

/* ---- Memory ---- */

void dave_free(void* ptr);

/* ---- Version ---- */

uint16_t dave_max_supported_protocol_version(void);

#ifdef __cplusplus
}
#endif

#endif /* LIBDAVE_C_H */
