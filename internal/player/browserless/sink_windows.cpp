// Native Windows audio sink.
//
// The browserless engine serves decrypted ADTS AAC on a loopback URL. Windows
// ships an AAC decoder in Media Foundation, so the sink pulls the stream with an
// IMFSourceReader (which inserts the decoder), applies volume and a 10-band
// equalizer to the PCM, and plays it through waveOut. Everything is in the OS:
// no codec, GStreamer or browser has to be installed.

#include "sink_windows.h"

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <mmsystem.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <mferror.h>

#include <atomic>
#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <cstring>
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
// Same centre frequencies as GStreamer's equalizer-10bands so the TUI's
// equalizer sounds alike on every platform.
constexpr double kBandHz[kBands] = {29, 59, 119, 237, 474, 947, 1889, 3770, 7523, 15011};
constexpr double kBandQ = 1.4;
constexpr int kNumBuffers = 8;
constexpr size_t kAACPrimingFrames = 2112;

// Only the .aac streams carry AAC priming; library uploads are .mp3 or .m4a.
bool isWholeTrackURI(const std::string& u) {
    return u.find(".aac") != std::string::npos && u.find("start_seg=") == std::string::npos;
}
// Media Foundation is resolved at run time instead of linked in. On Windows N
// editions without the Media Feature Pack mfplat.dll and mfreadwrite.dll do not
// exist, and importing them would stop vibez.exe from starting at all, even when
// the browserless engine is not used.
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

// Biquad peaking filter (RBJ cookbook), transposed direct form II.
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

    double run(double x, int ch) {
        const double y = b0 * x + z1[ch];
        z1[ch] = b1 * x - a1 * y + z2[ch];
        z2[ch] = b2 * x - a2 * y;
        return y;
    }
};

struct Session {
    std::atomic<bool> cancel{false};
    std::atomic<bool> paused{false};
    std::mutex mu;  // guards hwo
    HWAVEOUT hwo = nullptr;
    std::atomic<uint32_t> sampleRate{0};
    std::atomic<uint32_t> bytesPerSec{0};
    std::atomic<uint64_t> nullFrames{0};
    std::atomic<uint32_t> baseSamples{0};
    std::atomic<uint32_t> baseBytes{0};
    std::atomic<double> endPosition{-1.0};
};

double devicePosition(const Session& ses) {
    MMTIME t;
    std::memset(&t, 0, sizeof(t));
    t.wType = TIME_SAMPLES;
    if (waveOutGetPosition(ses.hwo, &t, sizeof(t)) != MMSYSERR_NOERROR) return 0.0;
    if (t.wType == TIME_SAMPLES && ses.sampleRate > 0) {
        const uint32_t base = ses.baseSamples.load();
        const uint32_t rel = t.u.sample >= base ? (t.u.sample - base) : 0;
        return static_cast<double>(rel) / static_cast<double>(ses.sampleRate);
    }
    if (t.wType == TIME_BYTES && ses.bytesPerSec > 0) {
        const uint32_t base = ses.baseBytes.load();
        const uint32_t rel = t.u.cb >= base ? (t.u.cb - base) : 0;
        return static_cast<double>(rel) / static_cast<double>(ses.bytesPerSec);
    }
    return 0.0;
}

struct Sink {
    uintptr_t user = 0;
    vibez_winsink_eos_cb_t onEos = nullptr;
    vibez_winsink_err_cb_t onErr = nullptr;
    vibez_winsink_stream_start_cb_t onStreamStart = nullptr;

    std::mutex mu;  // guards cur, nextURI, and eq
    std::shared_ptr<Session> cur;
    std::string nextURI;
    double eq[kBands] = {0};
    std::atomic<uint32_t> eqVersion{1};
    std::atomic<double> volume{1.0};
    std::atomic<bool> closed{false};
};

struct Handle {
    std::shared_ptr<Sink> sink;
};

std::wstring widen(const char* utf8) {
    int n = MultiByteToWideChar(CP_UTF8, 0, utf8, -1, nullptr, 0);
    if (n <= 0) return L"";
    std::wstring out(static_cast<size_t>(n), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, utf8, -1, &out[0], n);
    out.resize(static_cast<size_t>(n) - 1);
    return out;
}

void cancelSession(const std::shared_ptr<Session>& s) {
    if (!s) return;
    s->cancel = true;
    // Flush what is already queued so a skipped track stops at once instead of
    // playing out its buffered tail over the next one.
    std::lock_guard<std::mutex> lk(s->mu);
    if (s->hwo) waveOutReset(s->hwo);
}

void reportError(const std::shared_ptr<Sink>& sink, const std::shared_ptr<Session>& ses, const char* what, HRESULT hr) {
    if (sink->closed || ses->cancel) return;
    char msg[256];
    if (hr == MF_E_TOPO_CODEC_NOT_FOUND) {
        std::snprintf(msg, sizeof(msg),
                      "%s: no AAC decoder found (on Windows N editions install the Media Feature Pack)", what);
    } else {
        std::snprintf(msg, sizeof(msg), "%s (0x%08lx)", what, static_cast<unsigned long>(hr));
    }
    if (sink->onErr) sink->onErr(sink->user, msg);
}

struct Buf {
    WAVEHDR hdr;
    std::vector<char> data;
    bool queued = false;
};

// Waits until b is free to refill. Returns false if the session was cancelled.
bool waitFree(Buf& b, HWAVEOUT hwo, HANDLE ev, const Session& ses) {
    while (b.queued) {
        if (b.hdr.dwFlags & WHDR_DONE) {
            waveOutUnprepareHeader(hwo, &b.hdr, sizeof(WAVEHDR));
            b.queued = false;
            break;
        }
        if (ses.cancel) return false;
        WaitForSingleObject(ev, 50);
    }
    return true;
}

bool openSourceReader(const MfApi& mf, const std::shared_ptr<Sink>& sink, const std::shared_ptr<Session>& ses,
                      const std::wstring& url, Com<IMFSourceReader>& reader) {
    HRESULT hr = mf.createSourceReader(url.c_str(), nullptr, &reader);
    if (FAILED(hr)) {
        reportError(sink, ses, "cannot open audio stream", hr);
        return false;
    }
    reader->SetStreamSelection(MF_SOURCE_READER_ALL_STREAMS, FALSE);
    reader->SetStreamSelection(MF_SOURCE_READER_FIRST_AUDIO_STREAM, TRUE);

    Com<IMFMediaType> want;
    mf.createMediaType(&want);
    want->SetGUID(MF_MT_MAJOR_TYPE, MFMediaType_Audio);
    want->SetGUID(MF_MT_SUBTYPE, MFAudioFormat_PCM);
    want->SetUINT32(MF_MT_AUDIO_BITS_PER_SAMPLE, 16);
    hr = reader->SetCurrentMediaType(MF_SOURCE_READER_FIRST_AUDIO_STREAM, nullptr, want.p);
    if (FAILED(hr)) {
        reportError(sink, ses, "cannot decode audio stream", hr);
        return false;
    }
    return true;
}

void playLoop(const MfApi& mf, const std::shared_ptr<Sink>& sink, const std::shared_ptr<Session>& ses,
              std::string curURI) {
    Com<IMFSourceReader> reader;
    if (!openSourceReader(mf, sink, ses, widen(curURI.c_str()), reader)) return;

    HRESULT hr = S_OK;
    Com<IMFMediaType> got;
    hr = reader->GetCurrentMediaType(MF_SOURCE_READER_FIRST_AUDIO_STREAM, &got);
    if (FAILED(hr)) return reportError(sink, ses, "cannot read audio format", hr);

    WAVEFORMATEX* wfx = nullptr;
    UINT32 wfxSize = 0;
    hr = mf.createWaveFormat(got.p, &wfx, &wfxSize, MFWaveFormatExConvertFlag_Normal);
    if (FAILED(hr) || !wfx) return reportError(sink, ses, "cannot build wave format", hr);

    const uint32_t rate = wfx->nSamplesPerSec;
    const int channels = wfx->nChannels;
    const bool pcm16 = wfx->wBitsPerSample == 16 && wfx->wFormatTag == WAVE_FORMAT_PCM;
    ses->sampleRate = rate;
    ses->bytesPerSec = wfx->nAvgBytesPerSec;

    HANDLE ev = CreateEventW(nullptr, FALSE, FALSE, nullptr);
    HWAVEOUT hwo = nullptr;
    MMRESULT mm = waveOutOpen(&hwo, WAVE_MAPPER, wfx, reinterpret_cast<DWORD_PTR>(ev), 0, CALLBACK_EVENT);
    CoTaskMemFree(wfx);
    wchar_t nullEnv[8] = {};
    bool nullOut = GetEnvironmentVariableW(L"VIBEZ_NULL_AUDIO", nullEnv, 8) == 1 && nullEnv[0] == L'1';
    if (mm != MMSYSERR_NOERROR) {
        if (!nullOut) {
            CloseHandle(ev);
            return reportError(sink, ses, "cannot open audio output device", static_cast<HRESULT>(mm));
        }
    } else if (nullOut) {
        waveOutClose(hwo);
        hwo = nullptr;
    }
    if (!nullOut) {
        std::lock_guard<std::mutex> lk(ses->mu);
        ses->hwo = hwo;
        // Pause() can arrive before the device is open (a seek while paused).
        if (ses->paused) waveOutPause(hwo);
    }
    std::vector<Buf> bufs(kNumBuffers);
    for (auto& b : bufs) std::memset(&b.hdr, 0, sizeof(WAVEHDR));

    Biquad bank[kBands];
    uint32_t eqSeen = 0;
    int next = 0;
    bool endOfStream = false;
    size_t trimRemaining = isWholeTrackURI(curURI) ? kAACPrimingFrames : 0;
    uint32_t enqueuedSamples = 0;
    uint32_t enqueuedBytes = 0;
    for (;;) {
        if (ses->cancel) break;

        DWORD flags = 0;
        Com<IMFSample> sample;
        hr = reader->ReadSample(MF_SOURCE_READER_FIRST_AUDIO_STREAM, 0, nullptr, &flags, nullptr, &sample);
        if (ses->cancel) break;
        if (FAILED(hr)) {
            reportError(sink, ses, "audio stream failed", hr);
            break;
        }
        if (flags & MF_SOURCE_READERF_ENDOFSTREAM) {
            std::string nextURI;
            {
                std::lock_guard<std::mutex> lk(sink->mu);
                nextURI = std::move(sink->nextURI);
                sink->nextURI.clear();
            }
            if (!nextURI.empty()) {
                Com<IMFSourceReader> nextReader;
                if (openSourceReader(mf, sink, ses, widen(nextURI.c_str()), nextReader)) {
                    if (reader.p) reader.p->Release();
                    reader.p = nextReader.p;
                    nextReader.p = nullptr;
                    curURI = std::move(nextURI);
                    trimRemaining = isWholeTrackURI(curURI) ? kAACPrimingFrames : 0;
                    ses->baseSamples = enqueuedSamples;
                    ses->baseBytes = enqueuedBytes;
                    ses->nullFrames = 0;
                    if (sink->onStreamStart) {
                        std::vector<char> u(curURI.begin(), curURI.end());
                        u.push_back('\0');
                        sink->onStreamStart(sink->user, u.data());
                    }
                    continue;
                }
                break;
            }
            endOfStream = true;
            break;
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

        const size_t frameBytes = sizeof(int16_t) * static_cast<size_t>(channels > 0 ? channels : 2);
        size_t skipBytes = 0;
        if (pcm16 && trimRemaining > 0 && frameBytes > 0) {
            const size_t totalFrames = static_cast<size_t>(len) / frameBytes;
            const size_t cut = trimRemaining < totalFrames ? trimRemaining : totalFrames;
            trimRemaining -= cut;
            skipBytes = cut * frameBytes;
            if (skipBytes >= static_cast<size_t>(len)) {
                mb->Unlock();
                continue;
            }
        }

        Buf& b = bufs[next];
        if (!nullOut && !waitFree(b, hwo, ev, *ses)) {
            mb->Unlock();
            break;
        }
        b.data.assign(reinterpret_cast<char*>(src + skipBytes), reinterpret_cast<char*>(src) + len);
        mb->Unlock();

        if (pcm16) {
            int16_t* s = reinterpret_cast<int16_t*>(b.data.data());
            const size_t frames = b.data.size() / frameBytes;

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

        if (nullOut) {
            const size_t frameSize = sizeof(int16_t) * static_cast<size_t>(channels > 0 ? channels : 2);
            const uint64_t frames = frameSize > 0 ? b.data.size() / frameSize : 0;
            ses->nullFrames.fetch_add(frames);
            while (ses->paused && !ses->cancel) {
                Sleep(20);
            }
            if (rate > 0 && frames > 0) {
                const DWORD ms = static_cast<DWORD>((frames * 1000ULL) / rate);
                if (ms > 0) Sleep(ms);
            }
            continue;
        }

        const size_t bufFrames = frameBytes > 0 ? b.data.size() / frameBytes : 0;
        enqueuedSamples += static_cast<uint32_t>(bufFrames);
        enqueuedBytes += static_cast<uint32_t>(b.data.size());
        b.hdr.lpData = b.data.data();
        b.hdr.dwBufferLength = static_cast<DWORD>(b.data.size());
        b.hdr.dwFlags = 0;
        if (waveOutPrepareHeader(hwo, &b.hdr, sizeof(WAVEHDR)) != MMSYSERR_NOERROR ||
            waveOutWrite(hwo, &b.hdr, sizeof(WAVEHDR)) != MMSYSERR_NOERROR) {
            reportError(sink, ses, "audio output failed", E_FAIL);
            break;
        }
        b.queued = true;
        next = (next + 1) % kNumBuffers;
    }

    if (!nullOut) {
        if (endOfStream) {
            // Let the tail finish before announcing the end of the track.
            for (auto& b : bufs) {
                while (b.queued && !ses->cancel && !(b.hdr.dwFlags & WHDR_DONE)) WaitForSingleObject(ev, 50);
            }
        }

        {
            std::lock_guard<std::mutex> lk(ses->mu);
            if (endOfStream && !ses->cancel) ses->endPosition = devicePosition(*ses);
            ses->hwo = nullptr;
        }
        waveOutReset(hwo);
        for (auto& b : bufs) {
            if (b.queued) waveOutUnprepareHeader(hwo, &b.hdr, sizeof(WAVEHDR));
        }
        waveOutClose(hwo);
    }
    CloseHandle(ev);
    if (endOfStream && !ses->cancel && !sink->closed && sink->onEos) sink->onEos(sink->user);
}

void runSession(std::shared_ptr<Sink> sink, std::shared_ptr<Session> ses, std::string url) {
    const MfApi* mf = mfApi();
    if (!mf) {
        reportError(sink, ses,
                    "Media Foundation is not installed (on Windows N editions install the Media Feature Pack)",
                    HRESULT_FROM_WIN32(ERROR_MOD_NOT_FOUND));
        return;
    }
    const HRESULT hrCo = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    const HRESULT hrMf = mf->startup(MF_VERSION, MFSTARTUP_FULL);
    if (SUCCEEDED(hrMf)) {
        playLoop(*mf, sink, ses, std::move(url));
        mf->shutdown();
    } else {
        reportError(sink, ses, "Media Foundation failed to start", hrMf);
    }
    if (SUCCEEDED(hrCo)) CoUninitialize();
}
Sink* sinkOf(vibez_winsink_t h) { return h ? static_cast<Handle*>(h)->sink.get() : nullptr; }

}  // namespace

extern "C" {

vibez_winsink_t vibez_winsink_create(
    uintptr_t user_data,
    vibez_winsink_eos_cb_t on_eos,
    vibez_winsink_err_cb_t on_err,
    vibez_winsink_stream_start_cb_t on_stream_start) {
    auto* h = new Handle();
    h->sink = std::make_shared<Sink>();
    h->sink->user = user_data;
    h->sink->onEos = on_eos;
    h->sink->onErr = on_err;
    h->sink->onStreamStart = on_stream_start;
    return h;
}

void vibez_winsink_play_uri(vibez_winsink_t handle, const char* uri_utf8) {
    if (!handle || !uri_utf8) return;
    auto sink = static_cast<Handle*>(handle)->sink;
    auto ses = std::make_shared<Session>();
    std::shared_ptr<Session> old;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        sink->nextURI.clear();
        old = sink->cur;
        sink->cur = ses;
    }
    cancelSession(old);
    // The worker owns shared references to the sink and session, so a stale
    // session blocked on the network can finish by itself after it is replaced.
    std::thread(runSession, sink, ses, std::string(uri_utf8)).detach();
}

void vibez_winsink_set_next_uri(vibez_winsink_t handle, const char* uri_utf8) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    std::lock_guard<std::mutex> lk(sink->mu);
    sink->nextURI = uri_utf8 ? uri_utf8 : "";
}

void vibez_winsink_play(vibez_winsink_t handle) {
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

void vibez_winsink_pause(vibez_winsink_t handle) {
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

void vibez_winsink_stop(vibez_winsink_t handle) {
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

void vibez_winsink_set_volume(vibez_winsink_t handle, double volume) {
    Sink* sink = sinkOf(handle);
    if (!sink) return;
    if (volume < 0.0) volume = 0.0;
    if (volume > 1.0) volume = 1.0;
    sink->volume = volume;
}

void vibez_winsink_set_eq(vibez_winsink_t handle, const double* gains_db, int count) {
    Sink* sink = sinkOf(handle);
    if (!sink || !gains_db) return;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        for (int i = 0; i < kBands; ++i) {
            double g = i < count ? gains_db[i] : 0.0;
            if (g > 24.0) g = 24.0;
            if (g < -24.0) g = -24.0;
            sink->eq[i] = g;
        }
    }
    sink->eqVersion++;
}

double vibez_winsink_position(vibez_winsink_t handle) {
    Sink* sink = sinkOf(handle);
    if (!sink) return 0.0;
    std::shared_ptr<Session> ses;
    {
        std::lock_guard<std::mutex> lk(sink->mu);
        ses = sink->cur;
    }
    if (!ses) return 0.0;
    std::lock_guard<std::mutex> lk(ses->mu);
    if (!ses->hwo) {
        const double end = ses->endPosition.load();
        if (end >= 0.0) return end;
        const uint64_t nf = ses->nullFrames.load();
        if (nf > 0 && ses->sampleRate > 0) {
            return static_cast<double>(nf) / static_cast<double>(ses->sampleRate);
        }
        return 0.0;
    }
    return devicePosition(*ses);
}

void vibez_winsink_destroy(vibez_winsink_t handle) {
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
