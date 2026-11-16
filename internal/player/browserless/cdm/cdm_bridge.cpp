#include "cdm_bridge.h"
#include "content_decryption_module.h"

#include <algorithm>
#include <atomic>
#include <iostream>
#include <limits>
#include <memory>
#include <new>
#include <vector>
#include <chrono>
#include <cstring>
#include <mutex>
#include <condition_variable>
#include <cstdlib>

#ifdef _WIN32
#include <windows.h>
#else
#include <dlfcn.h>
#include <sys/time.h>
#endif

namespace {

// Portable shared-library and wall-clock helpers. The CDM is a plain C-ABI
// shared object everywhere; only how we load it differs.
#ifdef _WIN32
void* load_library(const char* utf8_path) {
    int n = MultiByteToWideChar(CP_UTF8, 0, utf8_path, -1, nullptr, 0);
    if (n <= 0) return nullptr;
    std::vector<wchar_t> wide(static_cast<size_t>(n));
    MultiByteToWideChar(CP_UTF8, 0, utf8_path, -1, wide.data(), n);
    // Restrict implicit dependency resolution to System32 so CWD, PATH, and
    // writable DLL directories cannot hijack non-KnownDLL dependencies.
    return reinterpret_cast<void*>(LoadLibraryExW(wide.data(), nullptr, LOAD_LIBRARY_SEARCH_SYSTEM32));
}
void* load_symbol(void* lib, const char* name) {
    return reinterpret_cast<void*>(GetProcAddress(static_cast<HMODULE>(lib), name));
}
void unload_library(void* lib) { FreeLibrary(static_cast<HMODULE>(lib)); }
double wall_time_seconds() {
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    ULARGE_INTEGER u;
    u.LowPart = ft.dwLowDateTime;
    u.HighPart = ft.dwHighDateTime;
    // 100ns ticks since 1601-01-01 -> seconds since the Unix epoch.
    return static_cast<double>(u.QuadPart - 116444736000000000ULL) / 10000000.0;
}
#else
void* load_library(const char* path) { return dlopen(path, RTLD_NOW); }
void* load_symbol(void* lib, const char* name) { return dlsym(lib, name); }
void unload_library(void* lib) { dlclose(lib); }
double wall_time_seconds() {
    struct timeval tv;
    gettimeofday(&tv, nullptr);
    return static_cast<double>(tv.tv_sec) + static_cast<double>(tv.tv_usec) / 1000000.0;
}
#endif

class BufferImpl : public cdm::Buffer {
    std::vector<uint8_t> data_;
public:
    static constexpr uint32_t kMaxCdmBufferSize = 64u << 20;

    explicit BufferImpl(uint32_t cap) {
        if (cap > 0 && cap <= kMaxCdmBufferSize) {
            try {
                data_.resize(cap);
            } catch (...) {
                data_.clear();
            }
        }
    }
    bool allocatedOK(uint32_t expectedCap) const {
        return expectedCap == 0 || (expectedCap <= kMaxCdmBufferSize && data_.size() == expectedCap);
    }
    void Destroy() override { delete this; }
    uint32_t Capacity() const override { return static_cast<uint32_t>(data_.capacity()); }
    uint8_t* Data() override { return data_.data(); }
    void SetSize(uint32_t s) override {
        if (s > kMaxCdmBufferSize) {
            data_.clear();
            return;
        }
        try {
            data_.resize(s);
        } catch (...) {
            data_.clear();
        }
    }
    uint32_t Size() const override { return static_cast<uint32_t>(data_.size()); }
};

class DecryptedBlockImpl : public cdm::DecryptedBlock {
    cdm::Buffer* buffer_ = nullptr;
    int64_t ts_ = 0;
public:
    void SetDecryptedBuffer(cdm::Buffer* b) override {
        if (b == buffer_) return;
        if (buffer_) buffer_->Destroy();
        buffer_ = b;
    }
    cdm::Buffer* DecryptedBuffer() override { return buffer_; }
    void SetTimestamp(int64_t ts) override { ts_ = ts; }
    int64_t Timestamp() const override { return ts_; }
    ~DecryptedBlockImpl() {
        if (buffer_) buffer_->Destroy();
    }
};

class Host11Impl : public cdm::Host_11 {
public:
    static constexpr uint32_t kMaxSessionIdSize = 4096;

    std::mutex cv_m;
    std::condition_variable msg_cv;
    std::condition_variable update_cv;
    std::condition_variable cert_cv;
    std::condition_variable close_cv;

    std::mutex cb_done_m;
    std::condition_variable cb_done_cv;
    std::atomic<int> active_callbacks{0};

    std::atomic<bool> destroyed{false};
    bool initialized = false;
    uint32_t next_promise_id = 1;
    uint32_t cert_promise_id = 0;
    uint32_t session_promise_id = 0;
    uint32_t update_promise_id = 0;
    uint32_t close_promise_id = 0;

    std::string sessionId;
    std::vector<uint8_t> sessionMessage;
    bool hasMessage = false;
    bool sessionFailed = false;
    bool updateResolved = false;
    bool updateFailed = false;
    bool certResolved = false;
    bool certFailed = false;
    std::string activeUpdateSessionId;
    std::string closingSessionId;
    bool closeResolved = false;
    bool closeFailed = false;
    int usableKeys = 0;

    struct CallbackGuard {
        Host11Impl& host;
        explicit CallbackGuard(Host11Impl& h) : host(h) {
            host.active_callbacks.fetch_add(1, std::memory_order_acq_rel);
        }
        ~CallbackGuard() {
            if (host.active_callbacks.fetch_sub(1, std::memory_order_acq_rel) == 1) {
                std::lock_guard<std::mutex> lk(host.cb_done_m);
                host.cb_done_cv.notify_all();
            }
        }
    };

    uint32_t allocPromiseIdLocked() {
        uint32_t id = next_promise_id++;
        if (next_promise_id == 0) {
            next_promise_id = 1;
        }
        return id;
    }

    void markDestroyed() {
        std::lock_guard<std::mutex> lk(cv_m);
        destroyed.store(true, std::memory_order_release);
        certFailed = true;
        sessionFailed = true;
        updateFailed = true;
        closeFailed = true;
        std::fill(sessionMessage.begin(), sessionMessage.end(), static_cast<uint8_t>(0));
        std::vector<uint8_t>().swap(sessionMessage);
        std::string().swap(sessionId);
        std::string().swap(activeUpdateSessionId);
        std::string().swap(closingSessionId);
        msg_cv.notify_all();
        update_cv.notify_all();
        cert_cv.notify_all();
        close_cv.notify_all();
    }

    void waitCallbacksIdle() {
        std::unique_lock<std::mutex> lk(cb_done_m);
        cb_done_cv.wait_for(lk, std::chrono::milliseconds(250), [&] {
            return active_callbacks.load(std::memory_order_acquire) == 0;
        });
    }

    cdm::Buffer* Allocate(uint32_t capacity) override {
        CallbackGuard guard(*this);
        if (destroyed.load(std::memory_order_acquire) || capacity > BufferImpl::kMaxCdmBufferSize) {
            return nullptr;
        }
        try {
            auto* buf = new (std::nothrow) BufferImpl(capacity);
            if (!buf) return nullptr;
            if (!buf->allocatedOK(capacity)) {
                delete buf;
                return nullptr;
            }
            return buf;
        } catch (...) {
            return nullptr;
        }
    }

    void SetTimer(int64_t delay_ms, void* context) override {}

    cdm::Time GetCurrentWallTime() override {
        return static_cast<cdm::Time>(wall_time_seconds());
    }

    void OnInitialized(bool success) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire)) return;
        initialized = success;
    }

    void OnResolveKeyStatusPromise(uint32_t promise_id, cdm::KeyStatus key_status) override {}

    void OnResolveNewSessionPromise(uint32_t promise_id, const char* session_id, uint32_t session_id_size) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire)) return;
        if (session_promise_id == 0 || promise_id != session_promise_id) return;
        if (session_id && session_id_size > 0 && session_id_size <= kMaxSessionIdSize) {
            sessionId.assign(session_id, session_id_size);
        } else {
            sessionId.clear();
        }
        msg_cv.notify_all();
    }

    void OnResolvePromise(uint32_t promise_id) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire) || promise_id == 0) return;
        if (promise_id == cert_promise_id) {
            certResolved = true;
            cert_cv.notify_all();
        } else if (promise_id == update_promise_id) {
            updateResolved = true;
            update_cv.notify_all();
        } else if (promise_id == close_promise_id) {
            closeResolved = true;
            close_cv.notify_all();
        }
    }

    void OnRejectPromise(uint32_t promise_id, cdm::Exception exception, uint32_t system_code, const char* error_message, uint32_t error_message_size) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire) || promise_id == 0) return;
        if (promise_id == cert_promise_id) {
            certFailed = true;
            cert_cv.notify_all();
        } else if (promise_id == session_promise_id) {
            sessionFailed = true;
            msg_cv.notify_all();
        } else if (promise_id == update_promise_id) {
            updateFailed = true;
            update_cv.notify_all();
        } else if (promise_id == close_promise_id) {
            closeFailed = true;
            close_cv.notify_all();
        }
    }

    void OnSessionMessage(const char* session_id, uint32_t session_id_size, cdm::MessageType message_type, const char* message, uint32_t message_size) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire)) return;
        if (session_promise_id == 0) return;
        if (!sessionId.empty()) {
            if (!session_id || session_id_size != sessionId.size() ||
                std::memcmp(sessionId.data(), session_id, session_id_size) != 0) {
                return;
            }
        }
        if (message && message_size > 0 && message_size <= BufferImpl::kMaxCdmBufferSize) {
            sessionMessage.assign(message, message + message_size);
            hasMessage = true;
        } else {
            sessionMessage.clear();
            hasMessage = false;
        }
        msg_cv.notify_all();
    }

    void OnSessionKeysChange(const char* session_id, uint32_t session_id_size, bool has_additional_usable_key, const cdm::KeyInformation* keys_info, uint32_t keys_info_count) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire) || !keys_info || keys_info_count == 0) return;
        if (update_promise_id == 0) return;
        if (!activeUpdateSessionId.empty() && session_id && session_id_size > 0) {
            if (session_id_size != activeUpdateSessionId.size() ||
                std::memcmp(activeUpdateSessionId.data(), session_id, session_id_size) != 0) {
                return;
            }
        }
        for (uint32_t i = 0; i < keys_info_count; i++) {
            if (keys_info[i].status == cdm::kUsable) {
                usableKeys++;
            }
        }
    }

    void OnExpirationChange(const char* session_id, uint32_t session_id_size, cdm::Time new_expiry_time) override {}

    void OnSessionClosed(const char* session_id, uint32_t session_id_size) override {
        CallbackGuard guard(*this);
        std::lock_guard<std::mutex> lk(cv_m);
        if (destroyed.load(std::memory_order_acquire)) return;
        if (session_id && session_id_size > 0 &&
            !closingSessionId.empty() &&
            closingSessionId.size() == session_id_size &&
            std::memcmp(closingSessionId.data(), session_id, session_id_size) == 0) {
            closeResolved = true;
            close_cv.notify_all();
        }
    }

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

size_t bounded_strlen(const char* s, size_t max_len) {
    if (!s) return 0;
    size_t n = 0;
    while (n < max_len && s[n] != '\0') {
        ++n;
    }
    return n;
}

// Widevine CDM internal worker threads remain alive until process exit (since
// unload_library is intentionally skipped), so retired Host11Impl shells stay
// mapped in process-lifetime storage with destroyed=true to eliminate UAF.
std::mutex retired_hosts_m;
std::vector<std::unique_ptr<Host11Impl>> retired_hosts;

void retire_host(std::unique_ptr<Host11Impl> host) {
    if (!host) return;
    std::lock_guard<std::mutex> lk(retired_hosts_m);
    retired_hosts.push_back(std::move(host));
}

} // namespace

struct cdm_context {
    void* lib_handle = nullptr;
    cdm::ContentDecryptionModule_11* cdm11 = nullptr;
    std::unique_ptr<Host11Impl> host;
    // Reusable scratch vector for subsample conversion under Go-side CDM.mu lock.
    std::vector<cdm::SubsampleEntry> scratch_subs;
};

extern "C" {

cdm_context_t* cdm_context_create(const char* library_path) {
    if (!library_path) return nullptr;
    try {
        void* handle = load_library(library_path);
        if (!handle) {
            return nullptr;
        }

        using InitFunc = void (*)(void);
        using CreateFunc = void* (*)(int, const char*, uint32_t, GetCdmHostFunc, void*);

        InitFunc init_cdm = reinterpret_cast<InitFunc>(load_symbol(handle, "InitializeCdmModule_4"));
        CreateFunc create_cdm = reinterpret_cast<CreateFunc>(load_symbol(handle, "CreateCdmInstance"));

        if (!init_cdm) {
            init_cdm = reinterpret_cast<InitFunc>(load_symbol(handle, "InitializeCdmModule"));
        }

        if (!create_cdm) {
            unload_library(handle);
            return nullptr;
        }

        if (init_cdm) {
            init_cdm();
        }

        cdm_context_t* ctx = new (std::nothrow) cdm_context();
        if (!ctx) {
            unload_library(handle);
            return nullptr;
        }
        ctx->host.reset(new (std::nothrow) Host11Impl());
        if (!ctx->host) {
            delete ctx;
            unload_library(handle);
            return nullptr;
        }
        ctx->lib_handle = handle;

        std::string key_system = "com.widevine.alpha";
        void* raw = create_cdm(11, key_system.c_str(), static_cast<uint32_t>(key_system.size()), GetCdmHostCallback, ctx->host.get());
        if (!raw) {
            ctx->host->markDestroyed();
            ctx->host->waitCallbacksIdle();
            delete ctx;
            unload_library(handle);
            return nullptr;
        }

        ctx->cdm11 = static_cast<cdm::ContentDecryptionModule_11*>(raw);
        ctx->cdm11->Initialize(true, false, false);

        return ctx;
    } catch (...) {
        return nullptr;
    }
}

int cdm_context_set_server_certificate(cdm_context_t* ctx, const uint8_t* cert_data, uint32_t cert_size) {
    if (!ctx || !ctx->host || !ctx->cdm11 || !cert_data || cert_size == 0) return -1;
    try {
        uint32_t promise_id = 0;
        {
            std::lock_guard<std::mutex> lk(ctx->host->cv_m);
            if (ctx->host->destroyed.load(std::memory_order_acquire)) return -1;
            promise_id = ctx->host->allocPromiseIdLocked();
            ctx->host->cert_promise_id = promise_id;
            ctx->host->certResolved = false;
            ctx->host->certFailed = false;
        }
        ctx->cdm11->SetServerCertificate(promise_id, cert_data, cert_size);
        {
            std::unique_lock<std::mutex> lk(ctx->host->cv_m);
            bool ok = ctx->host->cert_cv.wait_for(lk, std::chrono::seconds(5), [&]{
                return ctx->host->destroyed.load(std::memory_order_acquire) || ctx->host->certResolved || ctx->host->certFailed;
            });
            ctx->host->cert_promise_id = 0;
            if (!ok || ctx->host->certFailed || ctx->host->destroyed.load(std::memory_order_acquire)) {
                return -2;
            }
        }
        return 0;
    } catch (...) {
        return -1;
    }
}

int cdm_context_generate_challenge(
    cdm_context_t* ctx,
    const uint8_t* kid,
    uint32_t kid_size,
    uint8_t** out_challenge,
    uint32_t* out_challenge_size,
    char** out_session_id
) {
    if (!out_challenge || !out_challenge_size || !out_session_id) return -1;
    *out_challenge = nullptr;
    *out_challenge_size = 0;
    *out_session_id = nullptr;
    if (!ctx || !ctx->host || !ctx->cdm11 || !kid || kid_size != 16) return -1;

    try {
        uint32_t promise_id = 0;
        // Reset host session state
        {
            std::lock_guard<std::mutex> lk(ctx->host->cv_m);
            if (ctx->host->destroyed.load(std::memory_order_acquire)) return -1;
            promise_id = ctx->host->allocPromiseIdLocked();
            ctx->host->session_promise_id = promise_id;
            ctx->host->sessionId.clear();
            ctx->host->sessionMessage.clear();
            ctx->host->hasMessage = false;
            ctx->host->sessionFailed = false;
        }

        // Widevine PSSH header box (52 bytes) matching MusicKit v3 generatePSSH:
        //   [0..3]   size = 52 (32-byte box header + 4-byte protobuf prefix + 16-byte KID)
        //   [4..7]   "pssh"
        //   [8..11]  version = 0, flags = 0
        //   [12..27] Widevine SystemID: edef8ba9-79d6-4ace-a3c8-27dcd51d21ed
        //   [28..31] data_size = 20 (4 + 16)
        //   [32..35] WidevinePsshData protobuf: 0x08 0x01 (algorithm=AESCTR) 0x12 0x10 (key_id, len=16)
        //   [36..51] 16-byte Key ID
        std::vector<uint8_t> pssh = {
            0x00, 0x00, 0x00, 0x34,
            'p',  's',  's',  'h',
            0x00, 0x00, 0x00, 0x00,
            0xed, 0xef, 0x8b, 0xa9, 0x79, 0xd6, 0x4a, 0xce,
            0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed,
            0x00, 0x00, 0x00, 0x14,
            0x08, 0x01, 0x12, 0x10
        };
        pssh.insert(pssh.end(), kid, kid + kid_size);

        ctx->cdm11->CreateSessionAndGenerateRequest(
            promise_id,
            cdm::SessionType::kTemporary,
            cdm::InitDataType::kCenc,
            pssh.data(),
            static_cast<uint32_t>(pssh.size())
        );

        {
            std::unique_lock<std::mutex> lk(ctx->host->cv_m);
            bool ok = ctx->host->msg_cv.wait_for(lk, std::chrono::seconds(5), [&]{
                return ctx->host->destroyed.load(std::memory_order_acquire) ||
                       (ctx->host->hasMessage && !ctx->host->sessionId.empty()) ||
                       ctx->host->sessionFailed;
            });
            ctx->host->session_promise_id = 0;
            if (!ok || ctx->host->sessionFailed || ctx->host->destroyed.load(std::memory_order_acquire)) {
                return -2;
            }
            if (ctx->host->sessionMessage.empty() || ctx->host->sessionId.empty()) {
                return -2;
            }

            const size_t challengeSize = ctx->host->sessionMessage.size();
            const size_t sidSize = ctx->host->sessionId.size();
            *out_challenge = static_cast<uint8_t*>(std::malloc(challengeSize));
            if (!*out_challenge) {
                return -3;
            }
            std::memcpy(*out_challenge, ctx->host->sessionMessage.data(), challengeSize);

            *out_session_id = static_cast<char*>(std::malloc(sidSize + 1));
            if (!*out_session_id) {
                std::free(*out_challenge);
                *out_challenge = nullptr;
                return -4;
            }
            std::memcpy(*out_session_id, ctx->host->sessionId.c_str(), sidSize + 1);
            *out_challenge_size = static_cast<uint32_t>(challengeSize);
        }

        return 0;
    } catch (...) {
        if (*out_challenge) {
            std::free(*out_challenge);
            *out_challenge = nullptr;
        }
        if (*out_session_id) {
            std::free(*out_session_id);
            *out_session_id = nullptr;
        }
        *out_challenge_size = 0;
        return -1;
    }
}

int cdm_context_update_session(
    cdm_context_t* ctx,
    const char* session_id,
    const uint8_t* license_data,
    uint32_t license_size
) {
    if (!ctx || !ctx->host || !ctx->cdm11 || !session_id || !license_data || license_size == 0) return -1;
    try {
        size_t sid_len = bounded_strlen(session_id, Host11Impl::kMaxSessionIdSize + 1);
        if (sid_len == 0 || sid_len > Host11Impl::kMaxSessionIdSize) return -1;

        uint32_t promise_id = 0;
        {
            std::lock_guard<std::mutex> lk(ctx->host->cv_m);
            if (ctx->host->destroyed.load(std::memory_order_acquire)) return -1;
            promise_id = ctx->host->allocPromiseIdLocked();
            ctx->host->update_promise_id = promise_id;
            ctx->host->activeUpdateSessionId.assign(session_id, sid_len);
            ctx->host->updateResolved = false;
            ctx->host->updateFailed = false;
            ctx->host->usableKeys = 0;
        }

        ctx->cdm11->UpdateSession(
            promise_id,
            session_id,
            static_cast<uint32_t>(sid_len),
            license_data,
            license_size
        );

        {
            std::unique_lock<std::mutex> lk(ctx->host->cv_m);
            bool ok = ctx->host->update_cv.wait_for(lk, std::chrono::seconds(5), [&]{
                return ctx->host->destroyed.load(std::memory_order_acquire) ||
                       ctx->host->updateResolved ||
                       ctx->host->updateFailed;
            });
            ctx->host->update_promise_id = 0;
            ctx->host->activeUpdateSessionId.clear();
            if (!ok || ctx->host->updateFailed || ctx->host->destroyed.load(std::memory_order_acquire)) {
                return -2;
            }
            if (ctx->host->usableKeys == 0) {
                return -3;
            }
        }

        return 0;
    } catch (...) {
        return -1;
    }
}

int cdm_context_close_session(cdm_context_t* ctx, const char* session_id) {
    if (!ctx || !ctx->host || !ctx->cdm11 || !session_id) return -1;
    try {
        size_t sid_len = bounded_strlen(session_id, Host11Impl::kMaxSessionIdSize + 1);
        if (sid_len == 0 || sid_len > Host11Impl::kMaxSessionIdSize) return -1;

        uint32_t promise_id = 0;
        {
            std::lock_guard<std::mutex> lk(ctx->host->cv_m);
            if (ctx->host->destroyed.load(std::memory_order_acquire)) return -1;
            promise_id = ctx->host->allocPromiseIdLocked();
            ctx->host->close_promise_id = promise_id;
            ctx->host->closeResolved = false;
            ctx->host->closeFailed = false;
            ctx->host->closingSessionId.assign(session_id, sid_len);
        }

        ctx->cdm11->CloseSession(
            promise_id,
            session_id,
            static_cast<uint32_t>(sid_len)
        );

        std::unique_lock<std::mutex> lk(ctx->host->cv_m);
        bool ok = ctx->host->close_cv.wait_for(lk, std::chrono::milliseconds(500), [&] {
            return ctx->host->destroyed.load(std::memory_order_acquire) ||
                   ctx->host->closeResolved ||
                   ctx->host->closeFailed;
        });
        ctx->host->close_promise_id = 0;
        ctx->host->closingSessionId.clear();
        if (!ok || ctx->host->closeFailed || ctx->host->destroyed.load(std::memory_order_acquire)) {
            return -2;
        }
        return 0;
    } catch (...) {
        return -1;
    }
}

static int decrypt_one_sample(
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
    if (!out_size) return -1;
    *out_size = 0;
    if (!ctx || !ctx->host || !ctx->cdm11 ||
        !key_id || key_id_size == 0 || !iv || iv_size == 0 ||
        !in_data || in_size == 0 || in_size > BufferImpl::kMaxCdmBufferSize || !out_decrypted) {
        return -1;
    }
    if (ctx->host->destroyed.load(std::memory_order_acquire)) return -1;
    if (num_subsamples > 65536u) return -1;
    if (num_subsamples > 0 && (!clear_bytes || !cipher_bytes)) return -1;

    cdm::InputBuffer_2 input = {};
    input.data = in_data;
    input.data_size = in_size;
    input.encryption_scheme = cdm::EncryptionScheme::kCenc;
    input.key_id = key_id;
    input.key_id_size = key_id_size;
    input.iv = iv;
    input.iv_size = iv_size;

    cdm::SubsampleEntry default_subsample = {0, in_size};
    if (num_subsamples > 0) {
        uint64_t total_sub_bytes = 0;
        ctx->scratch_subs.resize(num_subsamples);
        for (uint32_t i = 0; i < num_subsamples; ++i) {
            total_sub_bytes += clear_bytes[i];
            total_sub_bytes += cipher_bytes[i];
            if (total_sub_bytes > in_size) {
                return -1;
            }
            ctx->scratch_subs[i].clear_bytes = clear_bytes[i];
            ctx->scratch_subs[i].cipher_bytes = cipher_bytes[i];
        }
        input.subsamples = ctx->scratch_subs.data();
        input.num_subsamples = num_subsamples;
    } else {
        input.subsamples = &default_subsample;
        input.num_subsamples = 1;
    }

    DecryptedBlockImpl output;
    cdm::Status status = ctx->cdm11->Decrypt(input, &output);
    if (status != cdm::kSuccess) {
        return static_cast<int>(status);
    }

    cdm::Buffer* out_buf = output.DecryptedBuffer();
    if (!out_buf || !out_buf->Data() || out_buf->Size() == 0) {
        return -10;
    }

    if (out_buf->Size() > in_size) {
        return -11;
    }

    *out_size = out_buf->Size();
    std::memcpy(out_decrypted, out_buf->Data(), *out_size);

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
    if (!ctx || !ctx->cdm11) {
        if (out_size) *out_size = 0;
        return -1;
    }
    try {
        return decrypt_one_sample(
            ctx, key_id, key_id_size, iv, iv_size,
            in_data, in_size, clear_bytes, cipher_bytes,
            num_subsamples, out_decrypted, out_size
        );
    } catch (...) {
        if (out_size) *out_size = 0;
        return -1;
    }
}

int cdm_context_decrypt_batch(
    cdm_context_t* ctx,
    const uint8_t* key_id,
    uint32_t key_id_size,
    const uint8_t* ivs_data,
    uint32_t ivs_total_size,
    const uint32_t* iv_sizes,
    const uint8_t* in_data,
    uint32_t in_total_size,
    const uint32_t* in_sizes,
    const uint16_t* clear_bytes,
    const uint32_t* cipher_bytes,
    const uint32_t* sub_counts,
    uint32_t total_subsamples,
    uint32_t num_samples,
    uint8_t* out_decrypted,
    uint32_t* out_sizes
) {
    if (!ctx || !ctx->host || !ctx->cdm11 || !key_id || key_id_size == 0) return -1;
    if (ctx->host->destroyed.load(std::memory_order_acquire)) return -1;
    if (num_samples == 0) return 0;
    if (num_samples > 65536u || !ivs_data || !iv_sizes || !in_sizes || !out_sizes) return -1;
    if (in_total_size > 0 && (!in_data || !out_decrypted)) return -1;
    if (total_subsamples > 0 && (!clear_bytes || !cipher_bytes)) return -1;

    try {
        uint64_t iv_pos = 0;
        uint64_t in_pos = 0;
        uint64_t sub_pos = 0;
        uint64_t out_pos = 0;

        for (uint32_t s = 0; s < num_samples; ++s) {
            uint32_t iv_sz = iv_sizes[s];
            uint32_t in_sz = in_sizes[s];
            uint32_t n_sub = sub_counts ? sub_counts[s] : 0;

            if (iv_sz == 0 || iv_pos + iv_sz > ivs_total_size) return -1;
            if (in_pos + in_sz > in_total_size) return -1;
            if (n_sub > 65536u || sub_pos + n_sub > total_subsamples) return -1;

            if (in_sz == 0) {
                out_sizes[s] = 0;
                iv_pos += iv_sz;
                sub_pos += n_sub;
                continue;
            }
            if (out_pos + in_sz > in_total_size) return -1;

            uint32_t out_sz = 0;
            const uint16_t* sub_clr = n_sub > 0 ? (clear_bytes + sub_pos) : nullptr;
            const uint32_t* sub_cip = n_sub > 0 ? (cipher_bytes + sub_pos) : nullptr;
            int rc = decrypt_one_sample(
                ctx,
                key_id,
                key_id_size,
                ivs_data + iv_pos,
                iv_sz,
                in_data + in_pos,
                in_sz,
                sub_clr,
                sub_cip,
                n_sub,
                out_decrypted + out_pos,
                &out_sz
            );
            if (rc != 0) {
                return rc;
            }
            out_sizes[s] = out_sz;
            iv_pos += iv_sz;
            in_pos += in_sz;
            sub_pos += n_sub;
            out_pos += out_sz;
        }

        return 0;
    } catch (...) {
        return -1;
    }
}

void cdm_context_destroy(cdm_context_t* ctx) {
    if (!ctx) return;
    try {
        if (ctx->host) {
            ctx->host->markDestroyed();
        }
        if (ctx->cdm11) {
            ctx->cdm11->Destroy();
            ctx->cdm11 = nullptr;
        }
        if (ctx->host) {
            ctx->host->waitCallbacksIdle();
            retire_host(std::move(ctx->host));
        }
        std::vector<cdm::SubsampleEntry>().swap(ctx->scratch_subs);
    } catch (...) {
    }
    // We intentionally do not call unload_library here: Widevine CDM spins up
    // internal threads and TLS destructors that expect the library code to
    // remain mapped until process termination.
    ctx->lib_handle = nullptr;
    delete ctx;
}

} // extern "C"
