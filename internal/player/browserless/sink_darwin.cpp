// Native macOS audio sink using CoreAudio AudioFileStream and AudioQueue.
//
// AVPlayer requires an NSRunLoop / Cocoa event loop, which does not run in a
// Go CLI terminal application, and AVPlayer does not natively support progressive
// ADTS AAC over loopback HTTP.
// AudioFileStream + AudioQueue is Apple's framework designed for streaming
// compressed audio, running on internal CoreAudio threads with zero RunLoop dependency.

#include "sink_darwin.h"

#include <AudioToolbox/AudioToolbox.h>
#include <CoreFoundation/CoreFoundation.h>

#include <arpa/inet.h>
#include <netdb.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <unistd.h>

#include <algorithm>
#include <atomic>
#include <cmath>
#include <cstdlib>
#include <cstring>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

namespace {

constexpr int kBands = 10;
constexpr double kBandHz[kBands] = {29, 59, 119, 237, 474, 947, 1889, 3770, 7523, 15011};
constexpr double kBandQ = 1.4;
constexpr UInt32 kAACPrimingFrames = 2112;

// Only the .aac streams carry AAC priming; library uploads are .mp3 or .m4a.
static bool isWholeTrackURI(const std::string& url) {
    return url.find(".aac") != std::string::npos && url.find("start_seg=") == std::string::npos;
}

static AudioFileTypeID fileTypeHint(const std::string& url) {
    if (url.find(".mp3") != std::string::npos) return kAudioFileMP3Type;
    if (url.find(".m4a") != std::string::npos) return kAudioFileM4AType;
    return kAudioFileAAC_ADTSType;
}
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

struct Session;
struct Sink;

static void onPropertyFound(
    void *inClientData,
    AudioFileStreamID inAudioFileStream,
    AudioFileStreamPropertyID inPropertyID,
    AudioFileStreamPropertyFlags *ioFlags);

static void onPacketsParsed(
    void *inClientData,
    UInt32 inNumberBytes,
    UInt32 inNumberPackets,
    const void *inInputData,
    AudioStreamPacketDescription *inPacketDescriptions);

static void audioQueueCallback(
    void *inUserData,
    AudioQueueRef inAQ,
    AudioQueueBufferRef inBuffer);

struct Session : public std::enable_shared_from_this<Session> {
    static constexpr int kNumBuffers = 6;
    static constexpr UInt32 kBufferSize = 32768;
    static constexpr size_t kMaxPacketsPerBuffer = 512;

    std::atomic<bool> cancelled{false};
    std::atomic<bool> started{false};
    std::atomic<bool> paused{false};
    std::atomic<bool> eof{false};
    std::atomic<bool> eosReached{false};
    std::atomic<int> enqueuedCount{0};
    std::atomic<double> lastPosition{0.0};
    std::atomic<bool> nullOut{false};
    std::atomic<uint64_t> nullFrames{0};
    std::atomic<double> baseSampleTime{0.0};
    uint64_t enqueuedFrames = 0;
    UInt32 trimFramesRemaining = 0;
    int sock = -1;
    AudioFileStreamID streamID = nullptr;
    AudioQueueRef queue = nullptr;
    AudioConverterRef converter = nullptr;
    AudioStreamBasicDescription asbd{};
    AudioStreamBasicDescription pcmAsbd{};
    bool formatReady = false;
    std::vector<char> pendingMagicCookie;
    Biquad eqBank[kBands];
    uint32_t eqSeen = 0;
    Sink* parentSink = nullptr;

    AudioQueueBufferRef buffers[kNumBuffers] = {nullptr};
    std::mutex bufferMutex;
    std::condition_variable bufferCond;
    std::vector<AudioQueueBufferRef> freeBuffers;

    AudioQueueBufferRef curBuf = nullptr;
    UInt32 curBufBytes = 0;
    std::vector<AudioStreamPacketDescription> curPacketDescs;

    uintptr_t userData = 0;
    vibez_darwinsink_eos_cb_t onEos = nullptr;
    vibez_darwinsink_err_cb_t onErr = nullptr;
    vibez_darwinsink_stream_start_cb_t onStreamStart = nullptr;
    std::atomic<float> initialVolume{1.0f};
    ~Session() {
        cancel();
        cleanup();
    }

    void cancel() {
        bool wasCancelled = cancelled.exchange(true);
        if (wasCancelled) return;

        if (sock >= 0) {
            shutdown(sock, SHUT_RDWR);
            close(sock);
            sock = -1;
        }

        bufferCond.notify_all();

        if (queue) {
            AudioQueueStop(queue, true);
        }
    }

    void cleanup() {
        if (queue) {
            AudioQueueDispose(queue, true);
            queue = nullptr;
        }
        if (converter) {
            AudioConverterDispose(converter);
            converter = nullptr;
        }
        if (streamID) {
            AudioFileStreamClose(streamID);
            streamID = nullptr;
        }
        if (sock >= 0) {
            close(sock);
            sock = -1;
        }
    }

    void reportError(const std::string& msg) {
        if (cancelled.load()) return;
        if (onErr) {
            std::vector<char> errMsg(msg.begin(), msg.end());
            errMsg.push_back('\0');
            onErr(userData, errMsg.data());
        }
    }

    AudioQueueBufferRef getFreeBuffer() {
        std::unique_lock<std::mutex> lock(bufferMutex);
        bufferCond.wait(lock, [this] {
            return !freeBuffers.empty() || cancelled.load();
        });
        if (cancelled.load() || freeBuffers.empty()) {
            return nullptr;
        }
        AudioQueueBufferRef buf = freeBuffers.back();
        freeBuffers.pop_back();
        return buf;
    }

    void flushCurrentBuffer() {
        if (!curBuf || curBufBytes == 0 || !queue) return;

        curBuf->mAudioDataByteSize = curBufBytes;

        {
            std::lock_guard<std::mutex> lock(bufferMutex);
            enqueuedCount++;
        }

        OSStatus err = AudioQueueEnqueueBuffer(queue, curBuf, 0, nullptr);

        curBuf = nullptr;
        curBufBytes = 0;
        curPacketDescs.clear();

        if (err == noErr && !started.load()) {
            if (enqueuedCount.load() >= 2) {
                if (!paused.load()) {
                    AudioQueueStart(queue, NULL);
                }
                started = true;
            }
        }
    }
};

struct Sink {
    uintptr_t user = 0;
    vibez_darwinsink_eos_cb_t onEos = nullptr;
    vibez_darwinsink_err_cb_t onErr = nullptr;
    vibez_darwinsink_stream_start_cb_t onStreamStart = nullptr;

    std::atomic<float> volume{1.0f};
    std::atomic<bool> closed{false};
    std::mutex mu;
    std::string nextURI;
    double eq[kBands] = {0};
    std::atomic<uint32_t> eqVersion{1};
    std::shared_ptr<Session> curSession;
    std::shared_ptr<Session> currentSession() {
        std::lock_guard<std::mutex> lock(mu);
        return curSession;
    }
};

static void onPropertyFound(
    void *inClientData,
    AudioFileStreamID inAudioFileStream,
    AudioFileStreamPropertyID inPropertyID,
    AudioFileStreamPropertyFlags *ioFlags) {

    Session* ses = static_cast<Session*>(inClientData);
    if (!ses || ses->cancelled.load()) return;

    if (inPropertyID == kAudioFileStreamProperty_DataFormat) {
        AudioStreamBasicDescription asbd{};
        UInt32 asbdSize = sizeof(asbd);
        OSStatus err = AudioFileStreamGetProperty(inAudioFileStream, kAudioFileStreamProperty_DataFormat, &asbdSize, &asbd);
        if (err == noErr) {
            ses->asbd = asbd;

            AudioStreamBasicDescription pcm{};
            pcm.mSampleRate = asbd.mSampleRate > 0 ? asbd.mSampleRate : 44100.0;
            pcm.mFormatID = kAudioFormatLinearPCM;
            pcm.mFormatFlags = kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked;
            pcm.mChannelsPerFrame = asbd.mChannelsPerFrame > 0 ? asbd.mChannelsPerFrame : 2;
            pcm.mBitsPerChannel = 16;
            pcm.mBytesPerFrame = pcm.mChannelsPerFrame * 2;
            pcm.mFramesPerPacket = 1;
            pcm.mBytesPerPacket = pcm.mBytesPerFrame;
            ses->pcmAsbd = pcm;

            if (ses->converter) {
                AudioConverterDispose(ses->converter);
                ses->converter = nullptr;
            }
            err = AudioConverterNew(&ses->asbd, &ses->pcmAsbd, &ses->converter);
            if (err != noErr) {
                ses->reportError("Failed to create AudioConverter: " + std::to_string(err));
                return;
            }
            if (!ses->pendingMagicCookie.empty()) {
                AudioConverterSetProperty(ses->converter, kAudioConverterDecompressionMagicCookie,
                                          static_cast<UInt32>(ses->pendingMagicCookie.size()),
                                          ses->pendingMagicCookie.data());
                ses->pendingMagicCookie.clear();
            }

            if (!ses->queue && !ses->nullOut.load()) {
                err = AudioQueueNewOutput(&ses->pcmAsbd, audioQueueCallback, ses, NULL, NULL, 0, &ses->queue);
                if (err == noErr) {
                    std::lock_guard<std::mutex> lock(ses->bufferMutex);
                    for (int i = 0; i < Session::kNumBuffers; ++i) {
                        AudioQueueAllocateBuffer(ses->queue, Session::kBufferSize, &ses->buffers[i]);
                        ses->freeBuffers.push_back(ses->buffers[i]);
                    }
                    AudioQueueSetParameter(ses->queue, kAudioQueueParam_Volume, ses->initialVolume.load());
                    ses->formatReady = true;
                } else {
                    const char* nullEnv = std::getenv("VIBEZ_NULL_AUDIO");
                    if (nullEnv && std::strcmp(nullEnv, "1") == 0) {
                        ses->nullOut = true;
                        ses->started = true;
                        ses->formatReady = true;
                    } else {
                        ses->reportError("Failed to initialize AudioQueue: " + std::to_string(err));
                    }
                }
            } else {
                ses->formatReady = true;
            }
        }
    } else if (inPropertyID == kAudioFileStreamProperty_MagicCookieData) {
        UInt32 cookieSize = 0;
        Boolean writable = false;
        OSStatus err = AudioFileStreamGetPropertyInfo(inAudioFileStream, kAudioFileStreamProperty_MagicCookieData, &cookieSize, &writable);
        if (err == noErr && cookieSize > 0) {
            std::vector<char> cookie(cookieSize);
            err = AudioFileStreamGetProperty(inAudioFileStream, kAudioFileStreamProperty_MagicCookieData, &cookieSize, cookie.data());
            if (err == noErr) {
                if (ses->converter) {
                    AudioConverterSetProperty(ses->converter, kAudioConverterDecompressionMagicCookie,
                                              cookieSize, cookie.data());
                } else {
                    ses->pendingMagicCookie = std::move(cookie);
                }
            }
        }
    }
}

struct PacketContext {
    const void* data;
    UInt32 size;
    AudioStreamPacketDescription desc;
    bool consumed;
};

static OSStatus converterInputProc(
    AudioConverterRef inAudioConverter,
    UInt32* ioNumberDataPackets,
    AudioBufferList* ioData,
    AudioStreamPacketDescription** outDataPacketDescription,
    void* inUserData) {
    (void)inAudioConverter;
    PacketContext* ctx = static_cast<PacketContext*>(inUserData);
    if (ctx->consumed) {
        *ioNumberDataPackets = 0;
        return 100; // no more input in this single-packet call
    }
    ctx->consumed = true;
    *ioNumberDataPackets = 1;
    ioData->mBuffers[0].mData = const_cast<void*>(ctx->data);
    ioData->mBuffers[0].mDataByteSize = ctx->size;
    ioData->mBuffers[0].mNumberChannels = 0;
    if (outDataPacketDescription) {
        *outDataPacketDescription = &ctx->desc;
    }
    return noErr;
}

static void onPacketsParsed(
    void *inClientData,
    UInt32 inNumberBytes,
    UInt32 inNumberPackets,
    const void *inInputData,
    AudioStreamPacketDescription *inPacketDescriptions) {

    Session* ses = static_cast<Session*>(inClientData);
    if (!ses || ses->cancelled.load() || (!ses->queue && !ses->nullOut.load()) || !ses->converter || !ses->formatReady) return;
    const int channels = static_cast<int>(ses->pcmAsbd.mChannelsPerFrame);
    const double rate = ses->pcmAsbd.mSampleRate;

    if (ses->parentSink) {
        const uint32_t ver = ses->parentSink->eqVersion.load();
        if (ver != ses->eqSeen) {
            double gains[kBands];
            {
                std::lock_guard<std::mutex> lk(ses->parentSink->mu);
                std::memcpy(gains, ses->parentSink->eq, sizeof(gains));
            }
            for (int b = 0; b < kBands; ++b) ses->eqBank[b].design(rate, kBandHz[b], gains[b]);
            ses->eqSeen = ver;
        }
    }

    bool eqOn = channels > 0 && channels <= 2;
    if (eqOn) {
        eqOn = false;
        for (int b = 0; b < kBands; ++b) eqOn = eqOn || ses->eqBank[b].active;
    }

    int16_t pcmFrameBuf[4096];

    for (UInt32 i = 0; i < inNumberPackets; ++i) {
        if (ses->cancelled.load()) return;

        SInt64 packetOffset = inPacketDescriptions ? inPacketDescriptions[i].mStartOffset : 0;
        UInt32 packetSize = inPacketDescriptions ? inPacketDescriptions[i].mDataByteSize : inNumberBytes;
        if (packetSize == 0) continue;

        PacketContext pctx{};
        pctx.data = static_cast<const char*>(inInputData) + packetOffset;
        pctx.size = packetSize;
        pctx.desc.mStartOffset = 0;
        pctx.desc.mDataByteSize = packetSize;
        pctx.desc.mVariableFramesInPacket = inPacketDescriptions ? inPacketDescriptions[i].mVariableFramesInPacket : 0;
        pctx.consumed = false;

        AudioBufferList outAbl{};
        outAbl.mNumberBuffers = 1;
        outAbl.mBuffers[0].mNumberChannels = ses->pcmAsbd.mChannelsPerFrame;
        outAbl.mBuffers[0].mDataByteSize = sizeof(pcmFrameBuf);
        outAbl.mBuffers[0].mData = pcmFrameBuf;

        UInt32 ioFrames = static_cast<UInt32>(sizeof(pcmFrameBuf)) / ses->pcmAsbd.mBytesPerFrame;
        OSStatus err = AudioConverterFillComplexBuffer(ses->converter, converterInputProc, &pctx, &ioFrames, &outAbl, nullptr);
        if ((err != noErr && err != 100) || ioFrames == 0) continue;

        UInt32 startFrame = 0;
        if (ses->trimFramesRemaining > 0) {
            UInt32 cut = std::min(ses->trimFramesRemaining, ioFrames);
            ses->trimFramesRemaining -= cut;
            startFrame = cut;
            ioFrames -= cut;
            if (ioFrames == 0) continue;
        }
        int16_t* pcmStart = pcmFrameBuf + startFrame * static_cast<UInt32>(channels);
        const UInt32 pcmBytes = ioFrames * ses->pcmAsbd.mBytesPerFrame;
        if (eqOn) {
            for (UInt32 f = 0; f < ioFrames; ++f) {
                for (int c = 0; c < channels; ++c) {
                    double x = pcmStart[f * channels + c];
                    for (int b = 0; b < kBands; ++b) {
                        if (ses->eqBank[b].active) x = ses->eqBank[b].run(x, c);
                    }
                    if (x > 32767.0) x = 32767.0;
                    if (x < -32768.0) x = -32768.0;
                    pcmStart[f * channels + c] = static_cast<int16_t>(x);
                }
            }
        }

        if (ses->nullOut.load()) {
            ses->nullFrames.fetch_add(ioFrames);
            if (rate > 0) {
                ses->lastPosition = static_cast<double>(ses->nullFrames.load()) / rate;
            }
            continue;
        }

        if (!ses->curBuf) {
            ses->curBuf = ses->getFreeBuffer();
            if (!ses->curBuf) return;
            ses->curBufBytes = 0;
        }
        if (ses->curBufBytes + pcmBytes > Session::kBufferSize) {
            ses->flushCurrentBuffer();
            ses->curBuf = ses->getFreeBuffer();
            if (!ses->curBuf) return;
            ses->curBufBytes = 0;
        }
        std::memcpy(static_cast<char*>(ses->curBuf->mAudioData) + ses->curBufBytes, pcmStart, pcmBytes);
        ses->curBufBytes += pcmBytes;
        ses->enqueuedFrames += ioFrames;
    }
}

static void audioQueueCallback(
    void *inUserData,
    AudioQueueRef inAQ,
    AudioQueueBufferRef inBuffer) {

    Session* ses = static_cast<Session*>(inUserData);
    if (!ses) return;

    {
        std::lock_guard<std::mutex> lock(ses->bufferMutex);
        ses->freeBuffers.push_back(inBuffer);
        ses->enqueuedCount--;
        if (ses->eof.load() && ses->enqueuedCount.load() == 0) {
            ses->eosReached = true;
        }
    }
    ses->bufferCond.notify_one();
}

static bool parseURL(const std::string& url, std::string& host, int& port, std::string& path) {
    std::string u = url;
    const std::string httpPrefix = "http://";
    if (u.find(httpPrefix) == 0) {
        u = u.substr(httpPrefix.length());
    }
    size_t slashPos = u.find('/');
    std::string hostPort;
    if (slashPos != std::string::npos) {
        hostPort = u.substr(0, slashPos);
        path = u.substr(slashPos);
    } else {
        hostPort = u;
        path = "/";
    }

    size_t colonPos = hostPort.find(':');
    if (colonPos != std::string::npos) {
        host = hostPort.substr(0, colonPos);
        try {
            port = std::stoi(hostPort.substr(colonPos + 1));
        } catch (...) {
            port = 80;
        }
    } else {
        host = hostPort;
        port = 80;
    }
    return true;
}

static bool streamOneURI(std::shared_ptr<Session> ses, const std::string& url) {
    std::string host, path;
    int port = 80;
    if (!parseURL(url, host, port, path)) {
        ses->reportError("Invalid stream URL");
        return false;
    }

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
        ses->reportError("Failed to create socket");
        return false;
    }
    ses->sock = sock;

    struct sockaddr_in sin{};
    sin.sin_family = AF_INET;
    sin.sin_port = htons(static_cast<uint16_t>(port));
    if (inet_pton(AF_INET, host.c_str(), &sin.sin_addr) <= 0) {
        ses->reportError("Invalid server host");
        return false;
    }

    struct timeval tv{};
    tv.tv_sec = 10;
    tv.tv_usec = 0;
    setsockopt(sock, SOL_SOCKET, SO_RCVTIMEO, reinterpret_cast<const char*>(&tv), sizeof(tv));
    setsockopt(sock, SOL_SOCKET, SO_SNDTIMEO, reinterpret_cast<const char*>(&tv), sizeof(tv));

    if (connect(sock, reinterpret_cast<struct sockaddr*>(&sin), sizeof(sin)) < 0) {
        if (!ses->cancelled.load()) {
            ses->reportError("Failed to connect to loopback audio stream");
        }
        return false;
    }

    std::string req = "GET " + path + " HTTP/1.0\r\n"
                    + "Host: " + host + "\r\n"
                    + "User-Agent: vibez/1.0\r\n"
                    + "Connection: close\r\n\r\n";

    if (send(sock, req.data(), req.size(), 0) < 0) {
        if (!ses->cancelled.load()) {
            ses->reportError("Failed to send HTTP request");
        }
        return false;
    }

    std::string headerStr;
    char c = 0;
    while (!ses->cancelled.load()) {
        ssize_t n = recv(sock, &c, 1, 0);
        if (n <= 0) {
            if (!ses->cancelled.load()) {
                ses->reportError("Stream connection closed before HTTP headers");
            }
            return false;
        }
        headerStr.push_back(c);
        if (headerStr.length() >= 4 &&
            headerStr.substr(headerStr.length() - 4) == "\r\n\r\n") {
            break;
        }
        if (headerStr.length() > 8192) {
            ses->reportError("Stream HTTP header exceeded size limit");
            return false;
        }
    }

    if (ses->cancelled.load()) return false;

    if (headerStr.find("200") == std::string::npos) {
        ses->reportError("Stream server returned error response: " + headerStr.substr(0, headerStr.find("\r\n")));
        return false;
    }

    bool isChunked = (headerStr.find("Transfer-Encoding: chunked") != std::string::npos ||
                      headerStr.find("transfer-encoding: chunked") != std::string::npos);

    if (ses->streamID) {
        AudioFileStreamClose(ses->streamID);
        ses->streamID = nullptr;
    }
    ses->trimFramesRemaining = isWholeTrackURI(url) ? kAACPrimingFrames : 0;

    OSStatus err = AudioFileStreamOpen(
        ses.get(),
        onPropertyFound,
        onPacketsParsed,
        fileTypeHint(url),
        &ses->streamID
    );
    if (err != noErr) {
        ses->reportError("Failed to create AudioFileStream: " + std::to_string(err));
        return false;
    }

    char buf[16384];
    if (!isChunked) {
        while (!ses->cancelled.load()) {
            ssize_t n = recv(sock, buf, sizeof(buf), 0);
            if (n <= 0) {
                break;
            }
            AudioFileStreamParseBytes(ses->streamID, static_cast<UInt32>(n), buf, 0);
        }
    } else {
        while (!ses->cancelled.load()) {
            std::string line;
            while (!ses->cancelled.load()) {
                char ch = 0;
                if (recv(sock, &ch, 1, 0) <= 0) break;
                if (ch == '\n') break;
                if (ch != '\r') line.push_back(ch);
            }
            if (line.empty()) break;
            size_t semi = line.find(';');
            if (semi != std::string::npos) line = line.substr(0, semi);
            unsigned long chunkSize = 0;
            try {
                chunkSize = std::stoul(line, nullptr, 16);
            } catch (...) {
                break;
            }
            if (chunkSize == 0) break;

            unsigned long remaining = chunkSize;
            while (remaining > 0 && !ses->cancelled.load()) {
                size_t toRead = std::min(static_cast<size_t>(remaining), sizeof(buf));
                ssize_t n = recv(sock, buf, toRead, 0);
                if (n <= 0) break;
                AudioFileStreamParseBytes(ses->streamID, static_cast<UInt32>(n), buf, 0);
                remaining -= n;
            }
            char crlf[2];
            recv(sock, crlf, 2, MSG_WAITALL);
        }
    }

    if (ses->sock >= 0) {
        close(ses->sock);
        ses->sock = -1;
    }
    if (ses->cancelled.load()) return false;

    AudioFileStreamParseBytes(ses->streamID, 0, NULL, kAudioFileStreamParseFlag_Discontinuity);
    return true;
}

static void runSession(std::shared_ptr<Session> ses, std::string url) {
    for (;;) {
        if (!streamOneURI(ses, url)) return;

        std::string next;
        if (ses->parentSink && !ses->cancelled.load()) {
            std::lock_guard<std::mutex> lk(ses->parentSink->mu);
            next = std::move(ses->parentSink->nextURI);
            ses->parentSink->nextURI.clear();
        }
        if (next.empty()) break;

        ses->baseSampleTime = static_cast<double>(ses->enqueuedFrames);
        ses->nullFrames = 0;
        ses->lastPosition = 0.0;
        if (ses->onStreamStart) {
            std::vector<char> uriBuf(next.begin(), next.end());
            uriBuf.push_back('\0');
            ses->onStreamStart(ses->userData, uriBuf.data());
        }
        url = std::move(next);
    }

    ses->flushCurrentBuffer();

    if (!ses->started.load() && ses->enqueuedCount.load() > 0 && ses->queue) {
        if (!ses->paused.load()) {
            AudioQueueStart(ses->queue, NULL);
        }
        ses->started = true;
    }

    {
        std::lock_guard<std::mutex> lock(ses->bufferMutex);
        ses->eof = true;
        if (ses->enqueuedCount.load() == 0) {
            ses->eosReached = true;
        }
    }

    std::unique_lock<std::mutex> lock(ses->bufferMutex);
    ses->bufferCond.wait(lock, [&] {
        return ses->eosReached.load() || ses->cancelled.load();
    });

    if (ses->eosReached.load() && !ses->cancelled.load()) {
        if (ses->onEos) {
            ses->onEos(ses->userData);
        }
    }
}

} // namespace

extern "C" {

vibez_darwinsink_t vibez_darwinsink_create(
    uintptr_t user_data,
    vibez_darwinsink_eos_cb_t on_eos,
    vibez_darwinsink_err_cb_t on_err,
    vibez_darwinsink_stream_start_cb_t on_stream_start) {
    Sink* sink = new Sink();
    sink->user = user_data;
    sink->onEos = on_eos;
    sink->onErr = on_err;
    sink->onStreamStart = on_stream_start;
    return static_cast<vibez_darwinsink_t>(sink);
}

void vibez_darwinsink_play_uri(vibez_darwinsink_t handle, const char* uri) {
    if (!handle || !uri) return;
    Sink* sink = static_cast<Sink*>(handle);
    if (sink->closed.load()) return;

    auto newSession = std::make_shared<Session>();
    newSession->parentSink = sink;
    newSession->userData = sink->user;
    newSession->onEos = sink->onEos;
    newSession->onErr = sink->onErr;
    newSession->onStreamStart = sink->onStreamStart;
    newSession->initialVolume = sink->volume.load();
    std::shared_ptr<Session> oldSession;
    {
        std::lock_guard<std::mutex> lock(sink->mu);
        sink->nextURI.clear();
        oldSession = sink->curSession;
        sink->curSession = newSession;
    }

    if (oldSession) {
        oldSession->cancel();
    }

    std::string urlStr(uri);
    std::thread([newSession, urlStr]() {
        runSession(newSession, urlStr);
    }).detach();
}

void vibez_darwinsink_set_next_uri(vibez_darwinsink_t handle, const char* uri) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    std::lock_guard<std::mutex> lock(sink->mu);
    sink->nextURI = uri ? uri : "";
}

void vibez_darwinsink_play(vibez_darwinsink_t handle) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    auto ses = sink->currentSession();
    if (!ses) return;
    ses->paused = false;
    if (ses->queue && ses->started.load()) {
        AudioQueueStart(ses->queue, NULL);
    }
}

void vibez_darwinsink_pause(vibez_darwinsink_t handle) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    auto ses = sink->currentSession();
    if (!ses) return;
    ses->paused = true;
    if (ses->queue && ses->started.load()) {
        AudioQueuePause(ses->queue);
    }
}

void vibez_darwinsink_stop(vibez_darwinsink_t handle) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    std::shared_ptr<Session> old;
    {
        std::lock_guard<std::mutex> lock(sink->mu);
        old = sink->curSession;
        sink->curSession.reset();
    }
    if (old) {
        old->cancel();
    }
}

void vibez_darwinsink_seek(vibez_darwinsink_t handle, double seconds) {
    // Player seeks by requesting the loopback stream with ?start_seg=N.
    (void)handle;
    (void)seconds;
}

void vibez_darwinsink_set_volume(vibez_darwinsink_t handle, float volume) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    if (volume < 0.0f) volume = 0.0f;
    if (volume > 1.0f) volume = 1.0f;
    sink->volume = volume;
    auto ses = sink->currentSession();
    if (ses && ses->queue) {
        AudioQueueSetParameter(ses->queue, kAudioQueueParam_Volume, volume);
    }
}

void vibez_darwinsink_set_eq(vibez_darwinsink_t handle, const double* gains_db, int count) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    {
        std::lock_guard<std::mutex> lock(sink->mu);
        for (int i = 0; i < kBands; ++i) {
            double g = (gains_db && i < count) ? gains_db[i] : 0.0;
            if (g > 24.0) g = 24.0;
            if (g < -24.0) g = -24.0;
            sink->eq[i] = g;
        }
    }
    sink->eqVersion++;
}

double vibez_darwinsink_position(vibez_darwinsink_t handle) {
    if (!handle) return 0.0;
    Sink* sink = static_cast<Sink*>(handle);
    auto ses = sink->currentSession();
    if (!ses || !ses->started.load()) return 0.0;
    if (ses->nullOut.load()) return ses->lastPosition.load();
    if (!ses->queue) return 0.0;
    AudioTimeStamp ts{};
    OSStatus err = AudioQueueGetCurrentTime(ses->queue, NULL, &ts, NULL);
    if (err == noErr && (ts.mFlags & kAudioTimeStampSampleTimeValid) && ses->pcmAsbd.mSampleRate > 0) {
        double relSamples = ts.mSampleTime - ses->baseSampleTime.load();
        double sec = relSamples / ses->pcmAsbd.mSampleRate;
        if (sec >= 0.0) {
            ses->lastPosition = sec;
            return sec;
        }
    }
    return ses->lastPosition.load();
}

void vibez_darwinsink_destroy(vibez_darwinsink_t handle) {
    if (!handle) return;
    Sink* sink = static_cast<Sink*>(handle);
    sink->closed = true;
    std::shared_ptr<Session> old;
    {
        std::lock_guard<std::mutex> lock(sink->mu);
        old = sink->curSession;
        sink->curSession.reset();
    }
    if (old) {
        old->cancel();
    }
    delete sink;
}

} // extern "C"
