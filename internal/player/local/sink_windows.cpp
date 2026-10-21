// Native Windows local audio sink using Media Foundation and waveOut.

#include "sink_windows.h"

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <mmsystem.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <mferror.h>
#include <propvarutil.h>

#include <atomic>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <future>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace {

#ifndef MF_E_TOPO_CODEC_NOT_FOUND
#define MF_E_TOPO_CODEC_NOT_FOUND ((HRESULT)0xC00D5212L)
#endif

constexpr int kBands = 10;
constexpr double kBandHz[kBands] = {29, 59, 119, 237, 474, 947, 1889, 3770, 7523, 15011};
constexpr double kBandQ = 1.4;
constexpr int kNumBuffers = 8;

struct MfApi {
    HRESULT(WINAPI* startup)(ULONG, DWORD) = nullptr;
    HRESULT(WINAPI* shutdown)() = nullptr;
    HRESULT(WINAPI* createMediaType)(IMFMediaType**) = nullptr;
    HRESULT(WINAPI* createWaveFormat)(IMFMediaType*, WAVEFORMATEX**, UINT32*, UINT32) = nullptr;
    HRESULT(WINAPI* createSourceReader)(LPCWSTR, IMFAttributes*, IMFSourceReader**) = nullptr;
};

template <class F>
F symbol(HMODULE mod, const char* name) {
    return reinterpret_cast<F>(reinterpret_cast<void*>(GetProcAddress(mod, name)));
}

const MfApi* mfApi() {
    static MfApi api;
    static const bool ok = [] {
        HMODULE plat = LoadLibraryW(L"mfplat.dll");
        HMODULE rw = LoadLibraryW(L"mfreadwrite.dll");
        if (!plat || !rw) return false;
        api.startup = symbol<decltype(api.startup)>(plat, "MFStartup");
        api.shutdown = symbol<decltype(api.shutdown)>(plat, "MFShutdown");
        api.createMediaType = symbol<decltype(api.createMediaType)>(plat, "MFCreateMediaType");
        api.createWaveFormat = symbol<decltype(api.createWaveFormat)>(plat, "MFCreateWaveFormatExFromMFMediaType");
        api.createSourceReader = symbol<decltype(api.createSourceReader)>(rw, "MFCreateSourceReaderFromURL");
        return api.startup && api.shutdown && api.createMediaType && api.createWaveFormat && api.createSourceReader;
    }();
    return ok ? &api : nullptr;
}

template <class T>
struct Com {
    T* p = nullptr;
    Com() = default;
    Com(const Com&) = delete;
    Com& operator=(const Com&) = delete;
    ~Com() { if (p) p->Release(); }
    T** operator&() { return &p; }
    T* operator->() const { return p; }
};

struct Biquad {
    double b0 = 1, b1 = 0, b2 = 0, a1 = 0, a2 = 0;
    double z1[2] = {0, 0}, z2[2] = {0, 0};
    bool active = false;

    void design(double fs, double f0, double gainDb) {
        if (std::fabs(gainDb) < 0.01 || f0 >= fs * 0.45) {
            active = false;
            return;
        }
        const double A = std::pow(10.0, gainDb / 40.0);
        const double w0 = 2.0 * 3.14159265358979323846 * f0 / fs;
        const double alpha = std::sin(w0) / (2.0 * kBandQ);
        const double cw = std::cos(w0);
        const double a0 = 1.0 + alpha / A;
        b0 = (1.0 + alpha * A) / a0;
        b1 = (-2.0 * cw) / a0;
        b2 = (1.0 - alpha * A) / a0;
        a1 = (-2.0 * cw) / a0;
        a2 = (1.0 - alpha / A) / a0;
        active = true;
    }

    void resetState() {
        z1[0] = z1[1] = 0;
        z2[0] = z2[1] = 0;
    }

    double run(double x, int ch) {
        const double y = b0 * x + z1[ch];
        z1[ch] = b1 * x - a1 * y + z2[ch];
        z2[ch] = b2 * x - a2 * y;
        return y;
    }
};

struct Session {
    uint64_t gen = 0;
    std::atomic<bool> cancel{false};
    std::atomic<bool> paused{false};
    std::atomic<double> seekTarget{-1.0};
    std::atomic<double> baseSec{0.0};
    std::atomic<double> durationSec{0.0};
    std::mutex mu;
    HWAVEOUT hwo = nullptr;
    HANDLE ev = nullptr;
    std::atomic<uint32_t> sampleRate{0};
    std::atomic<uint32_t> bytesPerSec{0};
    std::atomic<uint64_t> baseSamples{0};
    std::atomic<uint64_t> baseBytes{0};
};

struct Sink {
    uintptr_t user = 0;
    vibez_localsink_eos_cb_t onEos = nullptr;
    vibez_localsink_err_cb_t onErr = nullptr;

    std::mutex mu;
    std::shared_ptr<Session> cur;
    double eq[kBands] = {0};
    std::atomic<uint32_t> eqVersion{1};
    std::atomic<double> volume{1.0};
    std::atomic<bool> closed{false};
};

struct Handle {
    std::shared_ptr<Sink> sink;
};

struct OpenResult {
    bool ok = false;
    double durationSec = 0.0;
    std::string error;
};

std::wstring widen(const char* utf8) {
    int n = MultiByteToWideChar(CP_UTF8, 0, utf8, -1, nullptr, 0);
    if (n <= 0) return L"";
    std::wstring out(static_cast<size_t>(n), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, utf8, -1, &out[0], n);
    out.resize(static_cast<size_t>(n) - 1);
    return out;
}

std::string formatHr(const char* what, HRESULT hr) {
    char msg[256];
    if (hr == MF_E_TOPO_CODEC_NOT_FOUND) {
        std::snprintf(msg, sizeof(msg), "%s: unsupported format (on Windows N editions install the Media Feature Pack)", what);
    } else {
        std::snprintf(msg, sizeof(msg), "%s (0x%08lx)", what, static_cast<unsigned long>(hr));
    }
    return msg;
}

void cancelSession(const std::shared_ptr<Session>& s) {
    if (!s) return;
    s->cancel = true;
    std::lock_guard<std::mutex> lk(s->mu);
    if (s->hwo) waveOutReset(s->hwo);
    if (s->ev) SetEvent(s->ev);
}

void reportError(const std::shared_ptr<Sink>& sink, const std::shared_ptr<Session>& ses, const std::string& msg) {
    if (sink->closed || ses->cancel) return;
    if (sink->onErr) {
        std::vector<char> buf(msg.begin(), msg.end());
        buf.push_back('\0');
        sink->onErr(sink->user, ses->gen, buf.data());
    }
}

struct Buf {
    WAVEHDR hdr;
    std::vector<char> data;
    bool queued = false;
};

bool waitFree(Buf& b, HWAVEOUT hwo, HANDLE ev, const Session& ses) {
    while (b.queued) {
        if (b.hdr.dwFlags & WHDR_DONE) {
            waveOutUnprepareHeader(hwo, &b.hdr, sizeof(WAVEHDR));
            b.queued = false;
            break;
        }
        if (ses.cancel || ses.seekTarget.load() >= 0.0) return false;
        WaitForSingleObject(ev, 30);
    }
    return true;
}

double readDuration(IMFSourceReader* reader) {
    PROPVARIANT var;
    PropVariantInit(&var);
    double sec = 0.0;
    if (SUCCEEDED(reader->GetPresentationAttribute(MF_SOURCE_READER_MEDIASOURCE, MF_PD_DURATION, &var))) {
        if (var.vt == VT_UI8) {
            sec = static_cast<double>(var.uhVal.QuadPart) / 10000000.0;
        } else if (var.vt == VT_I8) {
            sec = static_cast<double>(var.hVal.QuadPart) / 10000000.0;
        }
        PropVariantClear(&var);
    }
    return sec;
}

void snapshotWaveOutBase(Session& ses, HWAVEOUT hwo) {
    MMTIME t;
    std::memset(&t, 0, sizeof(t));
    t.wType = TIME_SAMPLES;
    if (waveOutGetPosition(hwo, &t, sizeof(t)) == MMSYSERR_NOERROR) {
        if (t.wType == TIME_SAMPLES) ses.baseSamples = t.u.sample;
        else if (t.wType == TIME_BYTES) ses.baseBytes = t.u.cb;
    } else {
        ses.baseSamples = 0;
        ses.baseBytes = 0;
    }
}

void playLoop(const MfApi& mf, const std::shared_ptr<Sink>& sink, const std::shared_ptr<Session>& ses,
              const std::wstring& path, std::promise<OpenResult>& openProm) {
    bool promised = false;
    auto failOpen = [&](const std::string& msg) {
        if (!promised) {
            promised = true;
            openProm.set_value(OpenResult{false, 0.0, msg});
        } else {
            reportError(sink, ses, msg);
        }
    };

    Com<IMFSourceReader> reader;
    HRESULT hr = mf.createSourceReader(path.c_str(), nullptr, &reader);
    if (FAILED(hr)) return failOpen(formatHr("cannot open audio file", hr));

    reader->SetStreamSelection(MF_SOURCE_READER_ALL_STREAMS, FALSE);
    hr = reader->SetStreamSelection(MF_SOURCE_READER_FIRST_AUDIO_STREAM, TRUE);
    if (FAILED(hr)) return failOpen(formatHr("no audio stream found", hr));

    Com<IMFMediaType> want;
    mf.createMediaType(&want);
    want->SetGUID(MF_MT_MAJOR_TYPE, MFMediaType_Audio);
    want->SetGUID(MF_MT_SUBTYPE, MFAudioFormat_PCM);
    want->SetUINT32(MF_MT_AUDIO_BITS_PER_SAMPLE, 16);
    hr = reader->SetCurrentMediaType(MF_SOURCE_READER_FIRST_AUDIO_STREAM, nullptr, want.p);
    if (FAILED(hr)) return failOpen(formatHr("cannot decode audio stream", hr));

    Com<IMFMediaType> got;
    hr = reader->GetCurrentMediaType(MF_SOURCE_READER_FIRST_AUDIO_STREAM, &got);
    if (FAILED(hr)) return failOpen(formatHr("cannot read audio format", hr));

    WAVEFORMATEX* wfx = nullptr;
    UINT32 wfxSize = 0;
    hr = mf.createWaveFormat(got.p, &wfx, &wfxSize, MFWaveFormatExConvertFlag_Normal);
    if (FAILED(hr) || !wfx) return failOpen(formatHr("cannot build wave format", hr));

    const uint32_t rate = wfx->nSamplesPerSec;
    const int channels = wfx->nChannels;
    const bool pcm16 = wfx->wBitsPerSample == 16 && wfx->wFormatTag == WAVE_FORMAT_PCM;
    ses->sampleRate = rate;
    ses->bytesPerSec = wfx->nAvgBytesPerSec;
    const double dur = readDuration(reader.p);
    ses->durationSec = dur;

    HANDLE ev = CreateEventW(nullptr, FALSE, FALSE, nullptr);
    HWAVEOUT hwo = nullptr;
    MMRESULT mm = waveOutOpen(&hwo, WAVE_MAPPER, wfx, reinterpret_cast<DWORD_PTR>(ev), 0, CALLBACK_EVENT);
    CoTaskMemFree(wfx);
    if (mm != MMSYSERR_NOERROR) {
        CloseHandle(ev);
        return failOpen(formatHr("cannot open audio output device", static_cast<HRESULT>(mm)));
    }
    {
        std::lock_guard<std::mutex> lk(ses->mu);
        ses->hwo = hwo;
        ses->ev = ev;
        if (ses->paused) waveOutPause(hwo);
    }

    promised = true;
    openProm.set_value(OpenResult{true, dur, ""});

    std::vector<Buf> bufs(kNumBuffers);
    for (auto& b : bufs) std::memset(&b.hdr, 0, sizeof(WAVEHDR));

    Biquad bank[kBands];
    uint32_t eqSeen = 0;
    int next = 0;
    bool endOfStream = false;

    for (;;) {
        if (ses->cancel) break;

        const double seekSec = ses->seekTarget.exchange(-1.0);
        if (seekSec >= 0.0) {
            {
                std::lock_guard<std::mutex> lk(ses->mu);
                waveOutReset(hwo);
                for (auto& b : bufs) {
                    if (b.queued) {
                        waveOutUnprepareHeader(hwo, &b.hdr, sizeof(WAVEHDR));
                        b.queued = false;
                    }
                }
                snapshotWaveOutBase(*ses, hwo);
                ses->baseSec = seekSec;
                if (ses->paused) waveOutPause(hwo);
            }
            for (int i = 0; i < kBands; ++i) bank[i].resetState();
            PROPVARIANT posVar;
            PropVariantInit(&posVar);
            posVar.vt = VT_I8;
            posVar.hVal.QuadPart = static_cast<LONGLONG>(seekSec * 10000000.0);
            static constexpr GUID kZeroGuid = {};
            reader->SetCurrentPosition(kZeroGuid, posVar);
            PropVariantClear(&posVar);
            endOfStream = false;
            next = 0;
        }

        if (endOfStream) {
            bool anyQueued = false;
            for (auto& b : bufs) {
                if (b.queued && !(b.hdr.dwFlags & WHDR_DONE)) {
                    anyQueued = true;
                    break;
                }
            }
            if (!anyQueued) break;
            WaitForSingleObject(ev, 30);
            continue;
        }

        DWORD flags = 0;
        Com<IMFSample> sample;
        hr = reader->ReadSample(MF_SOURCE_READER_FIRST_AUDIO_STREAM, 0, nullptr, &flags, nullptr, &sample);
        if (ses->cancel) break;
        if (FAILED(hr)) {
            reportError(sink, ses, formatHr("audio playback failed", hr));
            break;
        }
        if (flags & MF_SOURCE_READERF_ENDOFSTREAM) {
            endOfStream = true;
            continue;
        }
        if (!sample.p) continue;

        Com<IMFMediaBuffer> mb;
        if (FAILED(sample->ConvertToContiguousBuffer(&mb))) continue;
        BYTE* src = nullptr;
        DWORD len = 0;
        if (FAILED(mb->Lock(&src, nullptr, &len)) || len == 0) {
            if (src) mb->Unlock();
            continue;
        }

        Buf& b = bufs[next];
        if (!waitFree(b, hwo, ev, *ses)) {
            mb->Unlock();
            if (ses->cancel) break;
            continue;
        }
        b.data.assign(reinterpret_cast<char*>(src), reinterpret_cast<char*>(src) + len);
        mb->Unlock();

        if (pcm16) {
            int16_t* s = reinterpret_cast<int16_t*>(b.data.data());
            const size_t frames = b.data.size() / (sizeof(int16_t) * static_cast<size_t>(channels));

            const uint32_t ver = sink->eqVersion.load();
            if (ver != eqSeen) {
                double gains[kBands];
                {
                    std::lock_guard<std::mutex> lk(sink->mu);
                    std::memcpy(gains, sink->eq, sizeof(gains));
                }
                for (int i = 0; i < kBands; ++i) bank[i].design(rate, kBandHz[i], gains[i]);
                eqSeen = ver;
            }

            bool eqOn = channels <= 2;
            if (eqOn) {
                eqOn = false;
                for (int i = 0; i < kBands; ++i) eqOn = eqOn || bank[i].active;
            }
            const double vol = sink->volume.load();
            if (eqOn || vol != 1.0) {
                for (size_t f = 0; f < frames; ++f) {
                    for (int c = 0; c < channels; ++c) {
                        double x = s[f * channels + c];
                        if (eqOn) {
                            for (int i = 0; i < kBands; ++i) {
                                if (bank[i].active) x = bank[i].run(x, c);
                            }
                        }
                        x *= vol;
                        if (x > 32767.0) x = 32767.0;
                        if (x < -32768.0) x = -32768.0;
                        s[f * channels + c] = static_cast<int16_t>(x);
                    }
                }
            }
        }

        b.hdr.lpData = b.data.data();
        b.hdr.dwBufferLength = static_cast<DWORD>(b.data.size());
        b.hdr.dwFlags = 0;
        if (waveOutPrepareHeader(hwo, &b.hdr, sizeof(WAVEHDR)) != MMSYSERR_NOERROR ||
            waveOutWrite(hwo, &b.hdr, sizeof(WAVEHDR)) != MMSYSERR_NOERROR) {
            reportError(sink, ses, "audio output failed");
            break;
        }
        b.queued = true;
        next = (next + 1) % kNumBuffers;
    }

    {
        std::lock_guard<std::mutex> lk(ses->mu);
        ses->hwo = nullptr;
        ses->ev = nullptr;
    }
    waveOutReset(hwo);
    for (auto& b : bufs) {
        if (b.queued) waveOutUnprepareHeader(hwo, &b.hdr, sizeof(WAVEHDR));
    }
    waveOutClose(hwo);
    CloseHandle(ev);

    if (endOfStream && !ses->cancel && !sink->closed && sink->onEos) {
        sink->onEos(sink->user, ses->gen);
    }
}

void runSession(std::shared_ptr<Sink> sink, std::shared_ptr<Session> ses, std::wstring path, std::promise<OpenResult> openProm) {
    const MfApi* mf = mfApi();
    if (!mf) {
        openProm.set_value(OpenResult{false, 0.0, "Media Foundation is not installed (on Windows N editions install the Media Feature Pack)"});
        return;
    }
    const HRESULT hrCo = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    const HRESULT hrMf = mf->startup(MF_VERSION, MFSTARTUP_FULL);
    if (SUCCEEDED(hrMf)) {
        playLoop(*mf, sink, ses, path, openProm);
        mf->shutdown();
    } else {
        openProm.set_value(OpenResult{false, 0.0, formatHr("Media Foundation failed to start", hrMf)});
    }
    if (SUCCEEDED(hrCo)) CoUninitialize();
}

Sink* sinkOf(vibez_localsink_t h) { return h ? static_cast<Handle*>(h)->sink.get() : nullptr; }

}  // namespace

extern "C" {

vibez_localsink_t vibez_localsink_create(uintptr_t user_data, vibez_localsink_eos_cb_t on_eos, vibez_localsink_err_cb_t on_err) {
    auto* h = new Handle();
    h->sink = std::make_shared<Sink>();
    h->sink->user = user_data;
    h->sink->onEos = on_eos;
    h->sink->onErr = on_err;
    return h;
}

int vibez_localsink_load(vibez_localsink_t handle, const char* path_utf8, uint64_t gen, int autoplay, double* out_duration_sec, char* err_buf, int err_buf_len) {
    if (!handle || !path_utf8) return -1;
    auto sink = static_cast<Handle*>(handle)->sink;
    auto ses = std::make_shared<Session>();
    ses->gen = gen;
    ses->paused = (autoplay == 0);

    std::shared_ptr<Session> old;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        old = sink->cur;
        sink->cur = ses;
    }
    cancelSession(old);

    std::promise<OpenResult> prom;
    auto fut = prom.get_future();
    std::thread(runSession, sink, ses, widen(path_utf8), std::move(prom)).detach();

    OpenResult res = fut.get();
    if (!res.ok) {
        if (err_buf && err_buf_len > 0) {
            std::snprintf(err_buf, static_cast<size_t>(err_buf_len), "%s", res.error.c_str());
        }
        return -1;
    }
    if (out_duration_sec) *out_duration_sec = res.durationSec;
    return 0;
}

void vibez_localsink_play(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    if (!ses) return;
    ses->paused = false;
    std::lock_guard<std::mutex> lk(ses->mu);
    if (ses->hwo) waveOutRestart(ses->hwo);
}

void vibez_localsink_pause(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    if (!ses) return;
    ses->paused = true;
    std::lock_guard<std::mutex> lk(ses->mu);
    if (ses->hwo) waveOutPause(ses->hwo);
}

void vibez_localsink_stop(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    if (!ses) return;
    ses->paused = true;
    ses->seekTarget = 0.0;
    ses->baseSec = 0.0;
    std::lock_guard<std::mutex> lk(ses->mu);
    if (ses->hwo) {
        waveOutPause(ses->hwo);
        snapshotWaveOutBase(*ses, ses->hwo);
    }
    if (ses->ev) SetEvent(ses->ev);
}

void vibez_localsink_seek(vibez_localsink_t handle, double seconds) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    if (!ses) return;
    if (seconds < 0.0) seconds = 0.0;
    ses->seekTarget = seconds;
    ses->baseSec = seconds;
    std::lock_guard<std::mutex> lk(ses->mu);
    if (ses->hwo) snapshotWaveOutBase(*ses, ses->hwo);
    if (ses->ev) SetEvent(ses->ev);
}

void vibez_localsink_set_volume(vibez_localsink_t handle, double volume) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    if (volume < 0.0) volume = 0.0;
    if (volume > 1.0) volume = 1.0;
    sink->volume = volume;
}

void vibez_localsink_set_eq(vibez_localsink_t handle, const double* gains_db, int count) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        for (int i = 0; i < kBands; ++i) {
            double g = (gains_db && i < count) ? gains_db[i] : 0.0;
            if (g > 24.0) g = 24.0;
            if (g < -24.0) g = -24.0;
            sink->eq[i] = g;
        }
    }
    sink->eqVersion++;
}

void vibez_localsink_unload(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    std::shared_ptr<Session> old;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        old = sink->cur;
        sink->cur.reset();
    }
    cancelSession(old);
}

double vibez_localsink_position(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return 0.0;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    if (!ses) return 0.0;
    const double pendingSeek = ses->seekTarget.load();
    if (pendingSeek >= 0.0) return pendingSeek;

    std::lock_guard<std::mutex> lk(ses->mu);
    const double base = ses->baseSec.load();
    if (!ses->hwo) return base;
    MMTIME t;
    std::memset(&t, 0, sizeof(t));
    t.wType = TIME_SAMPLES;
    if (waveOutGetPosition(ses->hwo, &t, sizeof(t)) != MMSYSERR_NOERROR) return base;
    if (t.wType == TIME_SAMPLES && ses->sampleRate > 0) {
        const uint64_t b = ses->baseSamples.load();
        const uint64_t cur = t.u.sample >= b ? (t.u.sample - b) : 0;
        return base + static_cast<double>(cur) / static_cast<double>(ses->sampleRate);
    }
    if (t.wType == TIME_BYTES && ses->bytesPerSec > 0) {
        const uint64_t b = ses->baseBytes.load();
        const uint64_t cur = t.u.cb >= b ? (t.u.cb - b) : 0;
        return base + static_cast<double>(cur) / static_cast<double>(ses->bytesPerSec);
    }
    return base;
}

double vibez_localsink_duration(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return 0.0;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    return ses ? ses->durationSec.load() : 0.0;
}

int vibez_localsink_is_playing(vibez_localsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return 0;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    return (ses && !ses->cancel && !ses->paused) ? 1 : 0;
}

void vibez_localsink_destroy(vibez_localsink_t handle) {
    if (!handle) return;
    auto* h = static_cast<Handle*>(handle);
    h->sink->closed = true;
    std::shared_ptr<Session> old;
    {
        std::lock_guard<std::mutex> lk(h->sink->mu);
        old = h->sink->cur;
        h->sink->cur.reset();
    }
    cancelSession(old);
    delete h;
}

}  // extern "C"
