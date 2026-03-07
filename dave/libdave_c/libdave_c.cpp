#include "libdave_c.h"

#include <cstdlib>
#include <cstring>
#include <memory>
#include <set>
#include <string>
#include <variant>
#include <vector>

#include "dave/mls/session.h"
#include "dave/encryptor.h"
#include "dave/decryptor.h"
#include "dave/key_ratchet.h"
#include "dave/common.h"
#include "dave/version.h"

/* Wrapper structs holding C++ objects */
struct dave_session_t {
    std::unique_ptr<discord::dave::mls::Session> session;
    std::shared_ptr<::mlspp::SignaturePrivateKey> transientKey;
};

struct dave_encryptor_t {
    std::unique_ptr<discord::dave::Encryptor> encryptor;
};

struct dave_decryptor_t {
    std::unique_ptr<discord::dave::Decryptor> decryptor;
};

struct dave_key_ratchet_t {
    std::unique_ptr<discord::dave::IKeyRatchet> ratchet;
};

/* Helper: copy bytes to malloc'd buffer */
static uint8_t* copy_to_malloc(const std::vector<uint8_t>& v, size_t* out_len) {
    if (v.empty()) {
        *out_len = 0;
        return nullptr;
    }
    *out_len = v.size();
    uint8_t* buf = static_cast<uint8_t*>(malloc(v.size()));
    if (buf) {
        memcpy(buf, v.data(), v.size());
    }
    return buf;
}

/* ---- MLS Session ---- */

dave_session_t* dave_session_create(const char* auth_session_id) {
    auto s = new dave_session_t();
    s->session = std::make_unique<discord::dave::mls::Session>(
        nullptr, /* KeyPairContextType: const char* on non-Android */
        auth_session_id ? std::string(auth_session_id) : std::string(),
        nullptr  /* MLSFailureCallback */
    );
    return s;
}

void dave_session_destroy(dave_session_t* s) {
    delete s;
}

void dave_session_init(dave_session_t* s,
                       uint16_t protocol_version,
                       uint64_t group_id,
                       const char* self_user_id) {
    if (!s || !s->session) return;
    s->session->Init(
        static_cast<discord::dave::ProtocolVersion>(protocol_version),
        group_id,
        self_user_id ? std::string(self_user_id) : std::string(),
        s->transientKey
    );
}

void dave_session_reset(dave_session_t* s) {
    if (!s || !s->session) return;
    s->session->Reset();
}

void dave_session_set_protocol_version(dave_session_t* s, uint16_t version) {
    if (!s || !s->session) return;
    s->session->SetProtocolVersion(static_cast<discord::dave::ProtocolVersion>(version));
}

uint16_t dave_session_get_protocol_version(dave_session_t* s) {
    if (!s || !s->session) return 0;
    return static_cast<uint16_t>(s->session->GetProtocolVersion());
}

uint8_t* dave_session_get_marshalled_key_package(dave_session_t* s,
                                                  size_t* out_len) {
    if (!s || !s->session || !out_len) return nullptr;
    auto kp = s->session->GetMarshalledKeyPackage();
    return copy_to_malloc(kp, out_len);
}

uint8_t* dave_session_process_proposals(dave_session_t* s,
                                         const uint8_t* proposals,
                                         size_t proposals_len,
                                         const char** recognized_user_ids,
                                         size_t num_user_ids,
                                         size_t* out_len) {
    if (!s || !s->session || !out_len) return nullptr;

    std::vector<uint8_t> proposalsVec(proposals, proposals + proposals_len);
    std::set<std::string> userIds;
    for (size_t i = 0; i < num_user_ids; i++) {
        if (recognized_user_ids[i]) {
            userIds.insert(recognized_user_ids[i]);
        }
    }

    auto result = s->session->ProcessProposals(std::move(proposalsVec), userIds);
    if (!result.has_value()) {
        *out_len = 0;
        return nullptr;
    }
    return copy_to_malloc(result.value(), out_len);
}

void dave_session_process_commit(dave_session_t* s,
                                  const uint8_t* commit,
                                  size_t commit_len,
                                  int* result_type,
                                  uint64_t** out_roster_ids,
                                  uint8_t*** out_roster_keys,
                                  size_t** out_roster_key_lens,
                                  size_t* out_roster_count) {
    if (!s || !s->session || !result_type) return;

    *result_type = DAVE_COMMIT_FAILED;
    if (out_roster_count) *out_roster_count = 0;

    std::vector<uint8_t> commitVec(commit, commit + commit_len);
    auto result = s->session->ProcessCommit(std::move(commitVec));

    if (std::holds_alternative<discord::dave::failed_t>(result)) {
        *result_type = DAVE_COMMIT_FAILED;
        return;
    }
    if (std::holds_alternative<discord::dave::ignored_t>(result)) {
        *result_type = DAVE_COMMIT_IGNORED;
        return;
    }

    *result_type = DAVE_COMMIT_OK;
    auto& roster = std::get<discord::dave::RosterMap>(result);

    size_t count = roster.size();
    if (out_roster_count) *out_roster_count = count;
    if (count == 0) return;

    if (out_roster_ids) {
        *out_roster_ids = static_cast<uint64_t*>(malloc(count * sizeof(uint64_t)));
    }
    if (out_roster_keys) {
        *out_roster_keys = static_cast<uint8_t**>(malloc(count * sizeof(uint8_t*)));
    }
    if (out_roster_key_lens) {
        *out_roster_key_lens = static_cast<size_t*>(malloc(count * sizeof(size_t)));
    }

    size_t i = 0;
    for (auto& [id, key] : roster) {
        if (out_roster_ids && *out_roster_ids) {
            (*out_roster_ids)[i] = id;
        }
        if (out_roster_keys && *out_roster_keys) {
            if (key.empty()) {
                (*out_roster_keys)[i] = nullptr;
            } else {
                (*out_roster_keys)[i] = static_cast<uint8_t*>(malloc(key.size()));
                memcpy((*out_roster_keys)[i], key.data(), key.size());
            }
        }
        if (out_roster_key_lens && *out_roster_key_lens) {
            (*out_roster_key_lens)[i] = key.size();
        }
        i++;
    }
}

int dave_session_process_welcome(dave_session_t* s,
                                  const uint8_t* welcome,
                                  size_t welcome_len,
                                  const char** recognized_user_ids,
                                  size_t num_user_ids,
                                  uint64_t** out_roster_ids,
                                  uint8_t*** out_roster_keys,
                                  size_t** out_roster_key_lens,
                                  size_t* out_roster_count) {
    if (!s || !s->session) return 0;

    if (out_roster_count) *out_roster_count = 0;

    std::vector<uint8_t> welcomeVec(welcome, welcome + welcome_len);
    std::set<std::string> userIds;
    for (size_t i = 0; i < num_user_ids; i++) {
        if (recognized_user_ids[i]) {
            userIds.insert(recognized_user_ids[i]);
        }
    }

    auto result = s->session->ProcessWelcome(std::move(welcomeVec), userIds);
    if (!result.has_value()) {
        return 0;
    }

    auto& roster = result.value();
    size_t count = roster.size();
    if (out_roster_count) *out_roster_count = count;
    if (count == 0) return 1;

    if (out_roster_ids) {
        *out_roster_ids = static_cast<uint64_t*>(malloc(count * sizeof(uint64_t)));
    }
    if (out_roster_keys) {
        *out_roster_keys = static_cast<uint8_t**>(malloc(count * sizeof(uint8_t*)));
    }
    if (out_roster_key_lens) {
        *out_roster_key_lens = static_cast<size_t*>(malloc(count * sizeof(size_t)));
    }

    size_t i = 0;
    for (auto& [id, key] : roster) {
        if (out_roster_ids && *out_roster_ids) {
            (*out_roster_ids)[i] = id;
        }
        if (out_roster_keys && *out_roster_keys) {
            if (key.empty()) {
                (*out_roster_keys)[i] = nullptr;
            } else {
                (*out_roster_keys)[i] = static_cast<uint8_t*>(malloc(key.size()));
                memcpy((*out_roster_keys)[i], key.data(), key.size());
            }
        }
        if (out_roster_key_lens && *out_roster_key_lens) {
            (*out_roster_key_lens)[i] = key.size();
        }
        i++;
    }
    return 1;
}

dave_key_ratchet_t* dave_session_get_key_ratchet(dave_session_t* s,
                                                   const char* user_id) {
    if (!s || !s->session || !user_id) return nullptr;
    auto ratchet = s->session->GetKeyRatchet(std::string(user_id));
    if (!ratchet) return nullptr;
    auto kr = new dave_key_ratchet_t();
    kr->ratchet = std::move(ratchet);
    return kr;
}

/* ---- Encryptor ---- */

dave_encryptor_t* dave_encryptor_create(void) {
    auto e = new dave_encryptor_t();
    e->encryptor = std::make_unique<discord::dave::Encryptor>();
    return e;
}

void dave_encryptor_destroy(dave_encryptor_t* e) {
    delete e;
}

void dave_encryptor_set_key_ratchet(dave_encryptor_t* e,
                                     dave_key_ratchet_t* kr) {
    if (!e || !e->encryptor || !kr) return;
    e->encryptor->SetKeyRatchet(std::move(kr->ratchet));
    delete kr;
}

void dave_encryptor_set_passthrough_mode(dave_encryptor_t* e, int enabled) {
    if (!e || !e->encryptor) return;
    e->encryptor->SetPassthroughMode(enabled != 0);
}

int dave_encryptor_is_passthrough_mode(dave_encryptor_t* e) {
    if (!e || !e->encryptor) return 1;
    return e->encryptor->IsPassthroughMode() ? 1 : 0;
}

size_t dave_encryptor_get_max_ciphertext_byte_size(dave_encryptor_t* e,
                                                     dave_media_type media_type,
                                                     size_t frame_size) {
    if (!e || !e->encryptor) return 0;
    return e->encryptor->GetMaxCiphertextByteSize(
        static_cast<discord::dave::MediaType>(media_type), frame_size);
}

int dave_encryptor_encrypt(dave_encryptor_t* e,
                            dave_media_type media_type,
                            uint32_t ssrc,
                            const uint8_t* frame,
                            size_t frame_len,
                            uint8_t* encrypted_frame,
                            size_t encrypted_frame_capacity,
                            size_t* bytes_written) {
    if (!e || !e->encryptor) return DAVE_RESULT_UNINITIALIZED_CONTEXT;

    discord::dave::ArrayView<const uint8_t> frameView(frame, frame_len);
    discord::dave::ArrayView<uint8_t> encView(encrypted_frame, encrypted_frame_capacity);

    return e->encryptor->Encrypt(
        static_cast<discord::dave::MediaType>(media_type),
        ssrc, frameView, encView, bytes_written);
}

/* ---- Decryptor ---- */

dave_decryptor_t* dave_decryptor_create(void) {
    auto d = new dave_decryptor_t();
    d->decryptor = std::make_unique<discord::dave::Decryptor>();
    return d;
}

void dave_decryptor_destroy(dave_decryptor_t* d) {
    delete d;
}

size_t dave_decryptor_get_max_plaintext_byte_size(dave_decryptor_t* d,
                                                    dave_media_type media_type,
                                                    size_t encrypted_frame_size) {
    if (!d || !d->decryptor) return 0;
    return d->decryptor->GetMaxPlaintextByteSize(
        static_cast<discord::dave::MediaType>(media_type), encrypted_frame_size);
}

size_t dave_decryptor_decrypt(dave_decryptor_t* d,
                               dave_media_type media_type,
                               const uint8_t* encrypted_frame,
                               size_t encrypted_frame_len,
                               uint8_t* frame,
                               size_t frame_capacity) {
    if (!d || !d->decryptor) return 0;

    discord::dave::ArrayView<const uint8_t> encView(encrypted_frame, encrypted_frame_len);
    discord::dave::ArrayView<uint8_t> frameView(frame, frame_capacity);

    return d->decryptor->Decrypt(
        static_cast<discord::dave::MediaType>(media_type), encView, frameView);
}

void dave_decryptor_transition_to_key_ratchet(dave_decryptor_t* d,
                                                dave_key_ratchet_t* kr,
                                                uint32_t transition_expiry_ms) {
    if (!d || !d->decryptor || !kr) return;

    if (transition_expiry_ms > 0) {
        d->decryptor->TransitionToKeyRatchet(
            std::move(kr->ratchet),
            std::chrono::milliseconds(transition_expiry_ms));
    } else {
        d->decryptor->TransitionToKeyRatchet(std::move(kr->ratchet));
    }
    delete kr;
}

void dave_decryptor_transition_to_passthrough_mode(dave_decryptor_t* d,
                                                     int passthrough,
                                                     uint32_t transition_expiry_ms) {
    if (!d || !d->decryptor) return;

    if (transition_expiry_ms > 0) {
        d->decryptor->TransitionToPassthroughMode(
            passthrough != 0,
            std::chrono::milliseconds(transition_expiry_ms));
    } else {
        d->decryptor->TransitionToPassthroughMode(passthrough != 0);
    }
}

/* ---- Key Ratchet ---- */

void dave_key_ratchet_destroy(dave_key_ratchet_t* kr) {
    delete kr;
}

/* ---- Memory ---- */

void dave_free(void* ptr) {
    free(ptr);
}

/* ---- Version ---- */

uint16_t dave_max_supported_protocol_version(void) {
    return static_cast<uint16_t>(discord::dave::MaxSupportedProtocolVersion());
}
