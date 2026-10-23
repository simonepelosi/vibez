// Windows System Media Transport Controls (SMTC) bridge.
//
// SMTC is what the media keys, the volume flyout and status-bar widgets such
// as YASB talk to. A Win32 process gets its controls from
// ISystemMediaTransportControlsInterop::GetForWindow, so this bridge owns a
// hidden top-level window on a dedicated STA thread that pumps messages, and
// applies every update on that thread.
//
// Why a window and not Windows.Media.Playback.MediaPlayer: a MediaPlayer
// would also hand out SMTC (with its CommandManager disabled), but activating
// it brings up a second Media Foundation playback pipeline that vibez never
// uses, while GetForWindow only needs user32 and the SMTC runtime class. It is
// the documented interop path for desktop apps, and the one Chromium and
// Firefox use. The window is a normal hidden top-level window: GetForWindow
// rejects message-only windows (HWND_MESSAGE).
//
// MinGW has no C++/WinRT, so the code calls the raw WinRT ABI, declared in
// smtc_winrt_abi_windows.h (see there for why not the MinGW headers). The WinRT
// entry points are resolved from combase.dll at run time, as the audio sink
// does for Media Foundation, so vibez.exe gains no WinRT import and a system
// without WinRT reports an error instead of failing to start.

#include "smtc_windows.h"

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "smtc_winrt_abi_windows.h"

#include <atomic>
#include <condition_variable>
#include <cstdio>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <utility>

namespace {

namespace abi = vibez_winrt;

constexpr UINT kMsgApply = WM_APP + 1;
constexpr UINT kMsgClose = WM_APP + 2;
constexpr wchar_t kWindowClass[] = L"VibezSMTCWindow";

struct WinRtApi {
    HRESULT(WINAPI* getActivationFactory)(HSTRING, REFIID, void**) = nullptr;
    HRESULT(WINAPI* activateInstance)(HSTRING, IInspectable**) = nullptr;
    HRESULT(WINAPI* createString)(PCNZWCH, UINT32, HSTRING*) = nullptr;
    HRESULT(WINAPI* deleteString)(HSTRING) = nullptr;
};

template <class F>
F symbol(HMODULE mod, const char* name) {
    return reinterpret_cast<F>(reinterpret_cast<void*>(GetProcAddress(mod, name)));
}

const WinRtApi* winrt() {
    static WinRtApi api;
    static const bool ok = [] {
        HMODULE combase = LoadLibraryExW(L"combase.dll", nullptr, LOAD_LIBRARY_SEARCH_SYSTEM32);
        if (!combase) return false;
        api.getActivationFactory = symbol<decltype(api.getActivationFactory)>(combase, "RoGetActivationFactory");
        api.activateInstance = symbol<decltype(api.activateInstance)>(combase, "RoActivateInstance");
        api.createString = symbol<decltype(api.createString)>(combase, "WindowsCreateString");
        api.deleteString = symbol<decltype(api.deleteString)>(combase, "WindowsDeleteString");
        return api.getActivationFactory && api.activateInstance && api.createString && api.deleteString;
    }();
    return ok ? &api : nullptr;
}

template <class T>
struct Com {
    T* p = nullptr;
    Com() = default;
    Com(const Com&) = delete;
    Com& operator=(const Com&) = delete;
    ~Com() { reset(); }
    void reset() {
        if (p) {
            p->Release();
            p = nullptr;
        }
    }
    T** put() {
        reset();
        return &p;
    }
    void** putVoid() { return reinterpret_cast<void**>(put()); }
    T* operator->() const { return p; }
    explicit operator bool() const { return p != nullptr; }
};

// HString owns an HSTRING. An empty string is the null HSTRING, which WinRT
// treats as "". If allocation fails the string is sent empty.
class HString {
public:
    explicit HString(const std::wstring& s) {
        if (!s.empty() && FAILED(winrt()->createString(s.c_str(), static_cast<UINT32>(s.size()), &h_))) h_ = nullptr;
    }
    HString(const HString&) = delete;
    HString& operator=(const HString&) = delete;
    ~HString() {
        if (h_) winrt()->deleteString(h_);
    }
    HSTRING get() const { return h_; }

private:
    HSTRING h_ = nullptr;
};

std::wstring widen(const char* s) {
    if (!s || !*s) return {};
    int n = MultiByteToWideChar(CP_UTF8, 0, s, -1, nullptr, 0);
    if (n <= 1) return {};
    std::wstring out(static_cast<size_t>(n - 1), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, s, -1, out.data(), n);
    return out;
}

// Gate lets destroy wait out event callbacks that are already running and
// turn away later ones. The handlers hold it by shared_ptr because WinRT may
// keep a handler alive after it is unregistered.
class Gate {
public:
    bool enter() {
        std::lock_guard<std::mutex> lock(mu_);
        if (!open_) return false;
        ++inflight_;
        return true;
    }
    void leave() {
        std::lock_guard<std::mutex> lock(mu_);
        if (--inflight_ == 0) cv_.notify_all();
    }
    void close() {
        std::unique_lock<std::mutex> lock(mu_);
        open_ = false;
        cv_.wait(lock, [this] { return inflight_ == 0; });
    }

private:
    std::mutex mu_;
    std::condition_variable cv_;
    bool open_ = true;
    int inflight_ = 0;
};

struct Callbacks {
    uintptr_t userData = 0;
    vibez_smtc_event_cb_t onEvent = nullptr;
    vibez_smtc_error_cb_t onError = nullptr;
    std::shared_ptr<Gate> gate = std::make_shared<Gate>();

    void event(int ev, int64_t positionHns) const {
        if (!onEvent || !gate->enter()) return;
        onEvent(userData, ev, positionHns);
        gate->leave();
    }
};

// EventHandler implements one ITypedEventHandler. It is agile, so SMTC calls it
// directly on its own thread instead of marshalling the call to the STA.
// IAgileObject has no methods of its own: answering its IID is the whole
// contract. The handler hands out its Iface pointer for it.
template <class Iface, const IID& kIID, class Derived>
class EventHandler : public Iface {
public:
    explicit EventHandler(const Callbacks& cb) : cb_(cb) {}
    virtual ~EventHandler() = default;

    HRESULT STDMETHODCALLTYPE QueryInterface(REFIID riid, void** out) override {
        if (!out) return E_POINTER;
        if (IsEqualIID(riid, abi::kIID_IUnknown) || IsEqualIID(riid, kIID) || IsEqualIID(riid, abi::kIID_IAgileObject)) {
            *out = static_cast<Iface*>(this);
            AddRef();
            return S_OK;
        }
        *out = nullptr;
        return E_NOINTERFACE;
    }
    ULONG STDMETHODCALLTYPE AddRef() override { return ++refs_; }
    ULONG STDMETHODCALLTYPE Release() override {
        ULONG n = --refs_;
        if (n == 0) delete static_cast<Derived*>(this);
        return n;
    }

protected:
    Callbacks cb_;

private:
    std::atomic<ULONG> refs_{1};
};

class ButtonPressed
    : public EventHandler<abi::IButtonPressedHandler, abi::kIID_ButtonPressedHandler, ButtonPressed> {
public:
    using EventHandler::EventHandler;
    HRESULT STDMETHODCALLTYPE Invoke(abi::ISystemMediaTransportControls*,
                                     abi::ISystemMediaTransportControlsButtonPressedEventArgs* args) override {
        if (!args) return S_OK;
        abi::SystemMediaTransportControlsButton button;
        if (FAILED(args->get_Button(&button))) return S_OK;
        int ev = 0;
        switch (button) {
        case abi::SystemMediaTransportControlsButton_Play: ev = VIBEZ_SMTC_EVENT_PLAY; break;
        case abi::SystemMediaTransportControlsButton_Pause: ev = VIBEZ_SMTC_EVENT_PAUSE; break;
        case abi::SystemMediaTransportControlsButton_Stop: ev = VIBEZ_SMTC_EVENT_STOP; break;
        case abi::SystemMediaTransportControlsButton_Next: ev = VIBEZ_SMTC_EVENT_NEXT; break;
        case abi::SystemMediaTransportControlsButton_Previous: ev = VIBEZ_SMTC_EVENT_PREVIOUS; break;
        default: return S_OK;
        }
        cb_.event(ev, 0);
        return S_OK;
    }
};

class PositionChangeRequested
    : public EventHandler<abi::IPositionChangeRequestedHandler, abi::kIID_PositionChangeRequestedHandler,
                          PositionChangeRequested> {
public:
    using EventHandler::EventHandler;
    HRESULT STDMETHODCALLTYPE Invoke(abi::ISystemMediaTransportControls*,
                                     abi::IPlaybackPositionChangeRequestedEventArgs* args) override {
        if (!args) return S_OK;
        abi::TimeSpan pos{};
        if (FAILED(args->get_RequestedPlaybackPosition(&pos))) return S_OK;
        cb_.event(VIBEZ_SMTC_EVENT_SEEK, pos.Duration);
        return S_OK;
    }
};

struct Pending {
    bool hasMetadata = false;
    std::wstring title, artist, album, artworkUrl;
    bool hasStatus = false;
    int status = VIBEZ_SMTC_STATUS_CLOSED;
    bool hasTimeline = false;
    int64_t positionHns = 0;
    int64_t durationHns = 0;

    bool empty() const { return !hasMetadata && !hasStatus && !hasTimeline; }
};

class Session {
public:
    explicit Session(const Callbacks& cb) : cb_(cb) {}

    // start runs the bridge thread and waits until it has the controls or
    // has failed.
    HRESULT start(const char** stage) {
        thread_ = std::thread([this] { run(); });
        HRESULT hr;
        {
            std::unique_lock<std::mutex> lock(initMu_);
            initCv_.wait(lock, [this] { return initDone_; });
            *stage = initStage_;
            hr = initHr_;
        }
        if (FAILED(hr)) thread_.join();
        return hr;
    }

    void queue(const vibez_smtc_update_t& u) {
        bool wake;
        {
            std::lock_guard<std::mutex> lock(mu_);
            wake = pending_.empty();
            if (u.has_metadata) {
                pending_.hasMetadata = true;
                pending_.title = widen(u.title);
                pending_.artist = widen(u.artist);
                pending_.album = widen(u.album);
                pending_.artworkUrl = widen(u.artwork_url);
            }
            if (u.has_status) {
                pending_.hasStatus = true;
                pending_.status = u.status;
            }
            if (u.has_timeline) {
                pending_.hasTimeline = true;
                pending_.positionHns = u.position_hns;
                pending_.durationHns = u.duration_hns;
            }
            wake = wake && !pending_.empty();
        }
        if (wake) PostMessageW(hwnd_, kMsgApply, 0, 0);
    }

    void stop() {
        cb_.gate->close();
        PostMessageW(hwnd_, kMsgClose, 0, 0);
        if (thread_.joinable()) thread_.join();
    }

private:
    static LRESULT CALLBACK wndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
        if (msg == WM_NCCREATE) {
            auto* cs = reinterpret_cast<CREATESTRUCTW*>(lp);
            SetWindowLongPtrW(hwnd, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(cs->lpCreateParams));
        }
        auto* s = reinterpret_cast<Session*>(GetWindowLongPtrW(hwnd, GWLP_USERDATA));
        switch (msg) {
        case kMsgApply:
            if (s) s->apply();
            return 0;
        case kMsgClose:
            if (s) s->teardown();
            DestroyWindow(hwnd);
            return 0;
        case WM_CLOSE:
            // Only kMsgClose ends the bridge; nothing else may take the window away.
            return 0;
        case WM_DESTROY:
            PostQuitMessage(0);
            return 0;
        default:
            return DefWindowProcW(hwnd, msg, wp, lp);
        }
    }

    void finishInit(HRESULT hr, const char* stage) {
        std::lock_guard<std::mutex> lock(initMu_);
        initHr_ = hr;
        initStage_ = stage;
        initDone_ = true;
        initCv_.notify_all();
    }

    void run() {
        HRESULT hr = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
        if (FAILED(hr)) {
            finishInit(hr, "CoInitializeEx");
            return;
        }
        const char* stage = "";
        hr = init(&stage);
        if (FAILED(hr)) {
            teardown();
            if (hwnd_) DestroyWindow(hwnd_);
            CoUninitialize();
            finishInit(hr, stage);
            return;
        }
        finishInit(S_OK, "");

        MSG msg;
        while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
            TranslateMessage(&msg);
            DispatchMessageW(&msg);
        }
        CoUninitialize();
    }

    HRESULT init(const char** stage) {
        if (!winrt()) {
            *stage = "loading WinRT from combase.dll";
            return HRESULT_FROM_WIN32(ERROR_PROC_NOT_FOUND);
        }
        HINSTANCE inst = GetModuleHandleW(nullptr);
        WNDCLASSEXW wc{};
        wc.cbSize = sizeof(wc);
        wc.lpfnWndProc = wndProc;
        wc.hInstance = inst;
        wc.lpszClassName = kWindowClass;
        if (!RegisterClassExW(&wc) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) {
            *stage = "RegisterClassExW";
            return HRESULT_FROM_WIN32(GetLastError());
        }
        hwnd_ = CreateWindowExW(WS_EX_TOOLWINDOW, kWindowClass, L"vibez", WS_OVERLAPPED, 0, 0, 0, 0, nullptr,
                                nullptr, inst, this);
        if (!hwnd_) {
            *stage = "CreateWindowExW";
            return HRESULT_FROM_WIN32(GetLastError());
        }

        Com<abi::ISystemMediaTransportControlsInterop> interop;
        HString cls(abi::kClassSystemMediaTransportControls);
        HRESULT hr = winrt()->getActivationFactory(cls.get(), abi::kIID_ISystemMediaTransportControlsInterop,
                                                   interop.putVoid());
        if (FAILED(hr)) {
            *stage = "RoGetActivationFactory(SystemMediaTransportControls)";
            return hr;
        }
        hr = interop->GetForWindow(hwnd_, abi::kIID_ISystemMediaTransportControls, smtc_.putVoid());
        if (FAILED(hr)) {
            *stage = "ISystemMediaTransportControlsInterop::GetForWindow";
            return hr;
        }
        hr = smtc_->QueryInterface(abi::kIID_ISystemMediaTransportControls2, smtc2_.putVoid());
        if (FAILED(hr)) {
            *stage = "QueryInterface(ISystemMediaTransportControls2)";
            return hr;
        }
        hr = smtc_->get_DisplayUpdater(updater_.put());
        if (FAILED(hr)) {
            *stage = "get_DisplayUpdater";
            return hr;
        }

        // The controls stay disabled, and so out of the system UI, until the
        // first track arrives.
        smtc_->put_IsEnabled(false);
        smtc_->put_IsPlayEnabled(true);
        smtc_->put_IsPauseEnabled(true);
        smtc_->put_IsStopEnabled(true);
        smtc_->put_IsNextEnabled(true);
        smtc_->put_IsPreviousEnabled(true);

        buttonHandler_ = new ButtonPressed(cb_);
        hr = smtc_->add_ButtonPressed(buttonHandler_, &buttonToken_);
        if (FAILED(hr)) {
            *stage = "add_ButtonPressed";
            return hr;
        }
        hasButtonToken_ = true;
        seekHandler_ = new PositionChangeRequested(cb_);
        hr = smtc2_->add_PlaybackPositionChangeRequested(seekHandler_, &seekToken_);
        if (FAILED(hr)) {
            *stage = "add_PlaybackPositionChangeRequested";
            return hr;
        }
        hasSeekToken_ = true;

        // The thumbnail factories are optional: without them there is no
        // artwork, and everything else still works.
        HString uriCls(abi::kClassUri);
        if (FAILED(winrt()->getActivationFactory(uriCls.get(), abi::kIID_IUriRuntimeClassFactory,
                                                 uriFactory_.putVoid()))) {
            uriFactory_.reset();
        }
        HString refCls(abi::kClassRandomAccessStreamReference);
        if (FAILED(winrt()->getActivationFactory(refCls.get(), abi::kIID_IRandomAccessStreamReferenceStatics,
                                                 streamRefs_.putVoid()))) {
            streamRefs_.reset();
        }
        return S_OK;
    }

    void teardown() {
        if (smtc_) {
            smtc_->put_PlaybackStatus(abi::MediaPlaybackStatus_Closed);
            smtc_->put_IsEnabled(false);
            if (hasButtonToken_) smtc_->remove_ButtonPressed(buttonToken_);
        }
        if (smtc2_ && hasSeekToken_) smtc2_->remove_PlaybackPositionChangeRequested(seekToken_);
        hasButtonToken_ = hasSeekToken_ = false;
        if (updater_) {
            updater_->ClearAll();
            updater_->Update();
        }
        if (buttonHandler_) buttonHandler_->Release();
        if (seekHandler_) seekHandler_->Release();
        buttonHandler_ = nullptr;
        seekHandler_ = nullptr;
        streamRefs_.reset();
        uriFactory_.reset();
        updater_.reset();
        smtc2_.reset();
        smtc_.reset();
    }

    void report(const char* step, HRESULT hr) {
        if (!cb_.onError) return;
        char msg[160];
        std::snprintf(msg, sizeof msg, "smtc: %s failed: HRESULT 0x%08lX", step, static_cast<unsigned long>(hr));
        cb_.onError(cb_.userData, msg);
    }

    void check(const char* step, HRESULT hr) {
        if (FAILED(hr)) report(step, hr);
    }

    void apply() {
        Pending p;
        {
            std::lock_guard<std::mutex> lock(mu_);
            p = std::move(pending_);
            pending_ = Pending{};
        }
        if (!smtc_) return;

        if (p.hasStatus && p.status == VIBEZ_SMTC_STATUS_CLOSED) {
            check("put_PlaybackStatus", smtc_->put_PlaybackStatus(abi::MediaPlaybackStatus_Closed));
            check("ClearAll", updater_->ClearAll());
            check("Update", updater_->Update());
            check("put_IsEnabled", smtc_->put_IsEnabled(false));
            return;
        }
        if (p.hasStatus) check("put_IsEnabled", smtc_->put_IsEnabled(true));
        if (p.hasMetadata) applyMetadata(p);
        if (p.hasStatus) {
            check("put_PlaybackStatus",
                  smtc_->put_PlaybackStatus(static_cast<abi::MediaPlaybackStatus>(p.status)));
        }
        if (p.hasTimeline) applyTimeline(p.positionHns, p.durationHns);
    }

    void applyMetadata(const Pending& p) {
        check("put_Type", updater_->put_Type(abi::MediaPlaybackType_Music));
        Com<abi::IMusicDisplayProperties> music;
        HRESULT hr = updater_->get_MusicProperties(music.put());
        if (FAILED(hr)) {
            report("get_MusicProperties", hr);
            return;
        }
        HString title(p.title), artist(p.artist), album(p.album);
        check("put_Title", music->put_Title(title.get()));
        check("put_Artist", music->put_Artist(artist.get()));
        Com<abi::IMusicDisplayProperties2> music2;
        if (SUCCEEDED(music->QueryInterface(abi::kIID_IMusicDisplayProperties2, music2.putVoid()))) {
            check("put_AlbumTitle", music2->put_AlbumTitle(album.get()));
        }
        applyThumbnail(p.artworkUrl);
        check("DisplayUpdater::Update", updater_->Update());
    }

    void applyThumbnail(const std::wstring& url) {
        bool web = url.rfind(L"https://", 0) == 0 || url.rfind(L"http://", 0) == 0;
        if (!web || !uriFactory_ || !streamRefs_) {
            check("put_Thumbnail", updater_->put_Thumbnail(nullptr));
            return;
        }
        // Windows downloads the image itself when the display is updated and
        // hands readers its copy.
        HString str(url);
        Com<IInspectable> uri;
        HRESULT hr = uriFactory_->CreateUri(str.get(), uri.put());
        if (FAILED(hr)) {
            report("CreateUri(artwork)", hr);
            check("put_Thumbnail", updater_->put_Thumbnail(nullptr));
            return;
        }
        Com<IInspectable> ref;
        hr = streamRefs_->CreateFromUri(uri.p, ref.put());
        if (FAILED(hr)) {
            report("RandomAccessStreamReference::CreateFromUri", hr);
            check("put_Thumbnail", updater_->put_Thumbnail(nullptr));
            return;
        }
        check("put_Thumbnail", updater_->put_Thumbnail(ref.p));
    }

    void applyTimeline(int64_t positionHns, int64_t durationHns) {
        HString cls(abi::kClassTimelineProperties);
        Com<IInspectable> inst;
        HRESULT hr = winrt()->activateInstance(cls.get(), inst.put());
        if (FAILED(hr)) {
            report("RoActivateInstance(SystemMediaTransportControlsTimelineProperties)", hr);
            return;
        }
        Com<abi::ISystemMediaTransportControlsTimelineProperties> tl;
        hr = inst->QueryInterface(abi::kIID_ISystemMediaTransportControlsTimelineProperties, tl.putVoid());
        if (FAILED(hr)) {
            report("QueryInterface(ISystemMediaTransportControlsTimelineProperties)", hr);
            return;
        }
        abi::TimeSpan zero{0}, end{durationHns}, pos{positionHns};
        tl->put_StartTime(zero);
        tl->put_EndTime(end);
        tl->put_MinSeekTime(zero);
        tl->put_MaxSeekTime(end);
        tl->put_Position(pos);
        check("UpdateTimelineProperties", smtc2_->UpdateTimelineProperties(tl.p));
    }

    Callbacks cb_;
    std::thread thread_;
    HWND hwnd_ = nullptr;

    std::mutex initMu_;
    std::condition_variable initCv_;
    bool initDone_ = false;
    HRESULT initHr_ = S_OK;
    const char* initStage_ = "";

    std::mutex mu_;
    Pending pending_;

    // Owned by the bridge thread.
    Com<abi::ISystemMediaTransportControls> smtc_;
    Com<abi::ISystemMediaTransportControls2> smtc2_;
    Com<abi::ISystemMediaTransportControlsDisplayUpdater> updater_;
    Com<abi::IUriRuntimeClassFactory> uriFactory_;
    Com<abi::IRandomAccessStreamReferenceStatics> streamRefs_;
    ButtonPressed* buttonHandler_ = nullptr;
    PositionChangeRequested* seekHandler_ = nullptr;
    EventRegistrationToken buttonToken_{};
    EventRegistrationToken seekToken_{};
    bool hasButtonToken_ = false;
    bool hasSeekToken_ = false;
};

} // namespace

extern "C" {

vibez_smtc_t vibez_smtc_create(uintptr_t user_data, vibez_smtc_event_cb_t on_event, vibez_smtc_error_cb_t on_error,
                               int32_t* hr_out, const char** stage_out) {
    Callbacks cb;
    cb.userData = user_data;
    cb.onEvent = on_event;
    cb.onError = on_error;
    auto* s = new Session(cb);
    const char* stage = "";
    HRESULT hr = s->start(&stage);
    if (hr_out) *hr_out = static_cast<int32_t>(hr);
    if (stage_out) *stage_out = stage;
    if (FAILED(hr)) {
        delete s;
        return nullptr;
    }
    return s;
}

void vibez_smtc_update(vibez_smtc_t handle, const vibez_smtc_update_t* u) {
    if (!handle || !u) return;
    static_cast<Session*>(handle)->queue(*u);
}

void vibez_smtc_destroy(vibez_smtc_t handle) {
    if (!handle) return;
    auto* s = static_cast<Session*>(handle);
    s->stop();
    delete s;
}

} // extern "C"
