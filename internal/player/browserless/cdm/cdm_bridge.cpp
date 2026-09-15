#include "cdm_bridge.h"
#include "content_decryption_module.h"

#include <iostream>
#include <vector>
#include <chrono>
#include <cstring>
#include <dlfcn.h>
#include <sys/time.h>
#include <mutex>
#include <condition_variable>
#include <cstdlib>

namespace {

class BufferImpl : public cdm::Buffer {
    std::vector<uint8_t> data_;
public:
    BufferImpl(uint32_t cap) : data_(cap) {}
    void Destroy() override { delete this; }
    uint32_t Capacity() const override { return static_cast<uint32_t>(data_.capacity()); }
    uint8_t* Data() override { return data_.data(); }
    void SetSize(uint32_t s) override { data_.resize(s); }
    uint32_t Size() const override { return static_cast<uint32_t>(data_.size()); }
};

class DecryptedBlockImpl : public cdm::DecryptedBlock {
    cdm::Buffer* buffer_ = nullptr;
    int64_t ts_ = 0;
public:
    void SetDecryptedBuffer(cdm::Buffer* b) override {
        if (buffer_) buffer_->Destroy();
        buffer_ = b;
    }
    cdm::Buffer* DecryptedBuffer() override { return buffer_; }
    void SetTimestamp(int64_t ts) override { ts_ = ts; }
    int64_t Timestamp() const override { return ts_; }
    ~DecryptedBlockImpl() override {
        if (buffer_) buffer_->Destroy();
    }
};

class Host11Impl : public cdm::Host_11 {
public:
    std::mutex cv_m;
    std::condition_variable msg_cv;
    std::condition_variable update_cv;
    std::condition_variable cert_cv;

    bool initialized = false;
    std::string sessionId;
    std::vector<uint8_t> sessionMessage;
    bool hasMessage = false;
    bool updateResolved = false;
    bool updateFailed = false;
    bool certResolved = false;
    bool certFailed = false;
    int usableKeys = 0;

    cdm::Buffer* Allocate(uint32_t capacity) override {
        return new BufferImpl(capacity);
    }

    void SetTimer(int64_t delay_ms, void* context) override {}

    cdm::Time GetCurrentWallTime() override {
        struct timeval tv;
        gettimeofday(&tv, nullptr);
        return static_cast<cdm::Time>(tv.tv_sec) + static_cast<cdm::Time>(tv.tv_usec) / 1000000.0;
    }

    void OnInitialized(bool success) override {
        std::lock_guard<std::mutex> lk(cv_m);
        initialized = success;
    }

    void OnResolveKeyStatusPromise(uint32_t promise_id, cdm::KeyStatus key_status) override {}

    void OnResolveNewSessionPromise(uint32_t promise_id, const char* session_id, uint32_t session_id_size) override {
        std::lock_guard<std::mutex> lk(cv_m);
        sessionId = std::string(session_id, session_id_size);
    }

    void OnResolvePromise(uint32_t promise_id) override {
        std::lock_guard<std::mutex> lk(cv_m);
        if (promise_id == 1) {
            certResolved = true;
            cert_cv.notify_all();
        } else if (promise_id == 3) {
            updateResolved = true;
            update_cv.notify_all();
        }
    }

    void OnRejectPromise(uint32_t promise_id, cdm::Exception exception, uint32_t system_code, const char* error_message, uint32_t error_message_size) override {
        std::lock_guard<std::mutex> lk(cv_m);
        if (promise_id == 1) {
            certFailed = true;
            cert_cv.notify_all();
        } else if (promise_id == 3) {
            updateFailed = true;
            update_cv.notify_all();
        }
    }

    void OnSessionMessage(const char* session_id, uint32_t session_id_size, cdm::MessageType message_type, const char* message, uint32_t message_size) override {
        std::lock_guard<std::mutex> lk(cv_m);
        sessionMessage.assign(message, message + message_size);
        hasMessage = true;
        msg_cv.notify_all();
    }

    void OnSessionKeysChange(const char* session_id, uint32_t session_id_size, bool has_additional_usable_key, const cdm::KeyInformation* keys_info, uint32_t keys_info_count) override {
        std::lock_guard<std::mutex> lk(cv_m);
        for (uint32_t i = 0; i < keys_info_count; i++) {
            if (keys_info[i].status == cdm::kUsable) {
                usableKeys++;
            }
        }
    }

    void OnExpirationChange(const char* session_id, uint32_t session_id_size, cdm::Time new_expiry_time) override {}

    void OnSessionClosed(const char* session_id, uint32_t session_id_size) override {}

    void SendPlatformChallenge(const char* service_id, uint32_t service_id_size, const char* challenge, uint32_t challenge_size) override {}

    void EnableOutputProtection(uint32_t desired_protection_mask) override {}

    void QueryOutputProtectionStatus() override {}

    void OnDeferredInitializationDone(cdm::StreamType stream_type, cdm::Status decoder_status) override {}

    cdm::FileIO* CreateFileIO(cdm::FileIOClient* client) override {
        return nullptr;
    }

    void RequestStorageId(uint32_t version) override {}

    void ReportMetrics(cdm::MetricName metric_name, uint64_t value) override {}
};

void* GetCdmHostCallback(int host_interface_version, void* user_data) {
    if (host_interface_version == 11) {
        return static_cast<cdm::Host_11*>(user_data);
    }
    return nullptr;
}

} // namespace

struct cdm_context {
    void* lib_handle = nullptr;
    cdm::ContentDecryptionModule_11* cdm11 = nullptr;
    Host11Impl host;
};

extern "C" {

cdm_context_t* cdm_context_create(const char* library_path) {
    if (!library_path) return nullptr;

    void* handle = dlopen(library_path, RTLD_NOW);
    if (!handle) {
        return nullptr;
    }

    using InitFunc = void (*)(void);
    using CreateFunc = void* (*)(int, const char*, uint32_t, GetCdmHostFunc, void*);

    InitFunc init_cdm = reinterpret_cast<InitFunc>(dlsym(handle, "InitializeCdmModule_4"));
    CreateFunc create_cdm = reinterpret_cast<CreateFunc>(dlsym(handle, "CreateCdmInstance"));

    if (!init_cdm) {
        init_cdm = reinterpret_cast<InitFunc>(dlsym(handle, "InitializeCdmModule"));
    }

    if (!create_cdm) {
        dlclose(handle);
        return nullptr;
    }

    if (init_cdm) {
        init_cdm();
    }

    cdm_context_t* ctx = new cdm_context();
    ctx->lib_handle = handle;

    std::string key_system = "com.widevine.alpha";
    void* raw = create_cdm(11, key_system.data(), static_cast<uint32_t>(key_system.size()), GetCdmHostCallback, &ctx->host);
    if (!raw) {
        delete ctx;
        dlclose(handle);
        return nullptr;
    }

    ctx->cdm11 = static_cast<cdm::ContentDecryptionModule_11*>(raw);
    ctx->cdm11->Initialize(true, false, false);

    return ctx;
}

int cdm_context_set_server_certificate(cdm_context_t* ctx, const uint8_t* cert_data, uint32_t cert_size) {
    if (!ctx || !ctx->cdm11) return -1;
    {
        std::lock_guard<std::mutex> lk(ctx->host.cv_m);
        ctx->host.certResolved = false;
        ctx->host.certFailed = false;
    }
    ctx->cdm11->SetServerCertificate(1, cert_data, cert_size);
    {
        std::unique_lock<std::mutex> lk(ctx->host.cv_m);
        bool ok = ctx->host.cert_cv.wait_for(lk, std::chrono::seconds(5), [&]{
            return ctx->host.certResolved || ctx->host.certFailed;
        });
        if (!ok || ctx->host.certFailed) {
            return -2;
        }
    }
    return 0;
}

int cdm_context_generate_challenge(
    cdm_context_t* ctx,
    const uint8_t* kid,
    uint32_t kid_size,
    uint8_t** out_challenge,
    uint32_t* out_challenge_size,
    char** out_session_id
) {
    if (!ctx || !ctx->cdm11 || !kid || kid_size != 16) return -1;

    // Reset host session state
    {
        std::lock_guard<std::mutex> lk(ctx->host.cv_m);
        ctx->host.sessionId.clear();
        ctx->host.sessionMessage.clear();
        ctx->host.hasMessage = false;
    }

    // Widevine PSSH header box for key ID
    std::vector<uint8_t> pssh;
    uint32_t pssh_data_size = 32 + kid_size;
    pssh.resize(pssh_data_size);

    pssh[3] = pssh_data_size; // size
    std::memcpy(&pssh[4], "pssh", 4);
    pssh[8] = 0; // version 0

    // System ID for Widevine: edef8ba9-79d6-4ace-a3c8-27dcd51d21ed
    const uint8_t widevine_uuid[16] = {
        0xed, 0xef, 0x8b, 0xa9, 0x79, 0xd6, 0x4a, 0xce,
        0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed
    };
    std::memcpy(&pssh[12], widevine_uuid, 16);

    pssh[31] = kid_size; // data size
    std::memcpy(&pssh[32], kid, kid_size);

    ctx->cdm11->CreateSessionAndGenerateRequest(
        2,
        cdm::SessionType::kTemporary,
        cdm::InitDataType::kCenc,
        pssh.data(),
        static_cast<uint32_t>(pssh.size())
    );

    {
        std::unique_lock<std::mutex> lk(ctx->host.cv_m);
        bool ok = ctx->host.msg_cv.wait_for(lk, std::chrono::seconds(5), [&]{
            return ctx->host.hasMessage && !ctx->host.sessionId.empty();
        });
        if (!ok) {
            return -2;
        }
    }

    {
        std::lock_guard<std::mutex> lk(ctx->host.cv_m);
        *out_challenge_size = static_cast<uint32_t>(ctx->host.sessionMessage.size());
        *out_challenge = static_cast<uint8_t*>(std::malloc(*out_challenge_size));
        if (!*out_challenge) {
            return -3;
        }
        std::memcpy(*out_challenge, ctx->host.sessionMessage.data(), *out_challenge_size);

        *out_session_id = strdup(ctx->host.sessionId.c_str());
        if (!*out_session_id) {
            std::free(*out_challenge);
            *out_challenge = nullptr;
            return -4;
        }
    }

    return 0;
}

int cdm_context_update_session(
    cdm_context_t* ctx,
    const char* session_id,
    const uint8_t* license_data,
    uint32_t license_size
) {
    if (!ctx || !ctx->cdm11 || !session_id || !license_data) return -1;

    {
        std::lock_guard<std::mutex> lk(ctx->host.cv_m);
        ctx->host.updateResolved = false;
        ctx->host.updateFailed = false;
        ctx->host.usableKeys = 0;
    }

    ctx->cdm11->UpdateSession(
        3,
        session_id,
        static_cast<uint32_t>(std::strlen(session_id)),
        license_data,
        license_size
    );

    {
        std::unique_lock<std::mutex> lk(ctx->host.cv_m);
        bool ok = ctx->host.update_cv.wait_for(lk, std::chrono::seconds(5), [&]{
            return ctx->host.updateResolved || ctx->host.updateFailed;
        });
        if (!ok || ctx->host.updateFailed) {
            return -2;
        }
        if (ctx->host.usableKeys == 0) {
            return -3;
        }
    }

    return 0;
}

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
) {
    if (!ctx || !ctx->cdm11 || !in_data || in_size == 0) return -1;

    cdm::InputBuffer_2 input = {};
    input.data = in_data;
    input.data_size = in_size;
    input.encryption_scheme = cdm::EncryptionScheme::kCenc;
    input.key_id = key_id;
    input.key_id_size = key_id_size;
    input.iv = iv;
    input.iv_size = iv_size;

    std::vector<cdm::SubsampleEntry> subs;
    if (num_subsamples > 0 && clear_bytes && cipher_bytes) {
        subs.resize(num_subsamples);
        for (uint32_t i = 0; i < num_subsamples; ++i) {
            subs[i].clear_bytes = clear_bytes[i];
            subs[i].cipher_bytes = cipher_bytes[i];
        }
        input.subsamples = subs.data();
        input.num_subsamples = num_subsamples;
    } else {
        cdm::SubsampleEntry default_subsample = {0, in_size};
        input.subsamples = &default_subsample;
        input.num_subsamples = 1;
    }

    DecryptedBlockImpl output;
    cdm::Status status = ctx->cdm11->Decrypt(input, &output);
    if (status != cdm::kSuccess) {
        return static_cast<int>(status);
    }

    if (!output.DecryptedBuffer() || output.DecryptedBuffer()->Size() == 0) {
        return -10;
    }

    if (output.DecryptedBuffer()->Size() > in_size) {
        return -11;
    }

    *out_size = output.DecryptedBuffer()->Size();
    std::memcpy(out_decrypted, output.DecryptedBuffer()->Data(), *out_size);

    return 0;
}

void cdm_context_destroy(cdm_context_t* ctx) {
    if (!ctx) return;
    if (ctx->cdm11) {
        ctx->cdm11->Destroy();
        ctx->cdm11 = nullptr;
    }
    if (ctx->lib_handle) {
        dlclose(ctx->lib_handle);
        ctx->lib_handle = nullptr;
    }
    delete ctx;
}

} // extern "C"
