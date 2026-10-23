// The slice of the WinRT ABI that the SMTC bridge calls, declared here rather
// than taken from the MinGW headers.
//
// The release job cross-compiles with Ubuntu 24.04's mingw-w64 11.0.1, whose
// windows.media.h has no System Media Transport Controls interfaces at all.
// mingw-w64 12 added ISystemMediaTransportControls, and only 13 added
// ISystemMediaTransportControls2, the timeline properties and
// IRandomAccessStreamReferenceStatics. Declaring the interfaces here builds
// the same on every one of them, and including none of those headers avoids
// their clashes, such as IReference<BYTE> and IReference<boolean> both being
// IReference<unsigned char> in 12 and later.
//
// The layouts and IIDs are those of the Windows SDK metadata
// (Windows.Media.winmd, Windows.Foundation.winmd, Windows.Storage.winmd). A
// published WinRT interface never changes, so they cannot drift. Methods the
// bridge does not call keep their vtable slot under their real name with
// opaque argument types.

#ifndef VIBEZ_SMTC_WINRT_ABI_H
#define VIBEZ_SMTC_WINRT_ABI_H

#include <windows.h>
#include <inspectable.h>
#include <eventtoken.h>

namespace vibez_winrt {

constexpr IID kIID_IUnknown = {0x00000000, 0x0000, 0x0000, {0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}};
constexpr IID kIID_IAgileObject = {0x94ea2b94, 0xe9cc, 0x49e0, {0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}};

// Windows.Foundation.TimeSpan, in 100 ns units.
struct TimeSpan {
    INT64 Duration;
};

// Windows.Media.MediaPlaybackStatus
enum MediaPlaybackStatus : INT32 {
    MediaPlaybackStatus_Closed = 0,
    MediaPlaybackStatus_Changing = 1,
    MediaPlaybackStatus_Stopped = 2,
    MediaPlaybackStatus_Playing = 3,
    MediaPlaybackStatus_Paused = 4,
};

// Windows.Media.MediaPlaybackType
enum MediaPlaybackType : INT32 {
    MediaPlaybackType_Unknown = 0,
    MediaPlaybackType_Music = 1,
};

// Windows.Media.SystemMediaTransportControlsButton
enum SystemMediaTransportControlsButton : INT32 {
    SystemMediaTransportControlsButton_Play = 0,
    SystemMediaTransportControlsButton_Pause = 1,
    SystemMediaTransportControlsButton_Stop = 2,
    SystemMediaTransportControlsButton_Record = 3,
    SystemMediaTransportControlsButton_FastForward = 4,
    SystemMediaTransportControlsButton_Rewind = 5,
    SystemMediaTransportControlsButton_Next = 6,
    SystemMediaTransportControlsButton_Previous = 7,
};

struct IMusicDisplayProperties;
struct ISystemMediaTransportControlsDisplayUpdater;
struct ISystemMediaTransportControlsTimelineProperties;
struct ISystemMediaTransportControlsButtonPressedEventArgs;
struct IPlaybackPositionChangeRequestedEventArgs;
struct ISystemMediaTransportControls;

// systemmediatransportcontrolsinterop.h
constexpr IID kIID_ISystemMediaTransportControlsInterop = {
    0xddb0472d, 0xc911, 0x4a1f, {0x86, 0xd9, 0xdc, 0x3d, 0x71, 0xa9, 0x5f, 0x5a}};
struct ISystemMediaTransportControlsInterop : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE GetForWindow(HWND appWindow, REFIID riid, void** mediaTransportControl) = 0;
};

// ITypedEventHandler<SystemMediaTransportControls, SystemMediaTransportControlsButtonPressedEventArgs>
constexpr IID kIID_ButtonPressedHandler = {0x0557e996, 0x7b23, 0x5bae, {0xaa, 0x81, 0xea, 0x0d, 0x67, 0x11, 0x43, 0xa4}};
struct IButtonPressedHandler : IUnknown {
    virtual HRESULT STDMETHODCALLTYPE Invoke(ISystemMediaTransportControls* sender,
                                             ISystemMediaTransportControlsButtonPressedEventArgs* args) = 0;
};

// ITypedEventHandler<SystemMediaTransportControls, PlaybackPositionChangeRequestedEventArgs>
constexpr IID kIID_PositionChangeRequestedHandler = {
    0x44e34f15, 0xbdc0, 0x50a7, {0xac, 0xe4, 0x39, 0xe9, 0x1f, 0xb7, 0x53, 0xf1}};
struct IPositionChangeRequestedHandler : IUnknown {
    virtual HRESULT STDMETHODCALLTYPE Invoke(ISystemMediaTransportControls* sender,
                                             IPlaybackPositionChangeRequestedEventArgs* args) = 0;
};

// Windows.Media.ISystemMediaTransportControls
constexpr IID kIID_ISystemMediaTransportControls = {
    0x99fa3ff4, 0x1742, 0x42a6, {0x90, 0x2e, 0x08, 0x7d, 0x41, 0xf9, 0x65, 0xec}};
struct ISystemMediaTransportControls : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_PlaybackStatus(MediaPlaybackStatus* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_PlaybackStatus(MediaPlaybackStatus value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_DisplayUpdater(ISystemMediaTransportControlsDisplayUpdater** value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_SoundLevel(INT32* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsPlayEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsPlayEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsStopEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsStopEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsPauseEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsPauseEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsRecordEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsRecordEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsFastForwardEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsFastForwardEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsRewindEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsRewindEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsPreviousEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsPreviousEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsNextEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsNextEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsChannelUpEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsChannelUpEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_IsChannelDownEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_IsChannelDownEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE add_ButtonPressed(IButtonPressedHandler* handler, EventRegistrationToken* token) = 0;
    virtual HRESULT STDMETHODCALLTYPE remove_ButtonPressed(EventRegistrationToken token) = 0;
    virtual HRESULT STDMETHODCALLTYPE add_PropertyChanged(IUnknown* handler, EventRegistrationToken* token) = 0;
    virtual HRESULT STDMETHODCALLTYPE remove_PropertyChanged(EventRegistrationToken token) = 0;
};

// Windows.Media.ISystemMediaTransportControls2
constexpr IID kIID_ISystemMediaTransportControls2 = {
    0xea98d2f6, 0x7f3c, 0x4af2, {0xa5, 0x86, 0x72, 0x88, 0x98, 0x08, 0xef, 0xb1}};
struct ISystemMediaTransportControls2 : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_AutoRepeatMode(INT32* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_AutoRepeatMode(INT32 value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_ShuffleEnabled(boolean* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_ShuffleEnabled(boolean value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_PlaybackRate(DOUBLE* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_PlaybackRate(DOUBLE value) = 0;
    virtual HRESULT STDMETHODCALLTYPE UpdateTimelineProperties(ISystemMediaTransportControlsTimelineProperties* props) = 0;
    virtual HRESULT STDMETHODCALLTYPE add_PlaybackPositionChangeRequested(IPositionChangeRequestedHandler* handler,
                                                                          EventRegistrationToken* token) = 0;
    virtual HRESULT STDMETHODCALLTYPE remove_PlaybackPositionChangeRequested(EventRegistrationToken token) = 0;
    virtual HRESULT STDMETHODCALLTYPE add_PlaybackRateChangeRequested(IUnknown* handler, EventRegistrationToken* token) = 0;
    virtual HRESULT STDMETHODCALLTYPE remove_PlaybackRateChangeRequested(EventRegistrationToken token) = 0;
    virtual HRESULT STDMETHODCALLTYPE add_ShuffleEnabledChangeRequested(IUnknown* handler, EventRegistrationToken* token) = 0;
    virtual HRESULT STDMETHODCALLTYPE remove_ShuffleEnabledChangeRequested(EventRegistrationToken token) = 0;
    virtual HRESULT STDMETHODCALLTYPE add_AutoRepeatModeChangeRequested(IUnknown* handler, EventRegistrationToken* token) = 0;
    virtual HRESULT STDMETHODCALLTYPE remove_AutoRepeatModeChangeRequested(EventRegistrationToken token) = 0;
};

// Windows.Media.ISystemMediaTransportControlsDisplayUpdater
struct ISystemMediaTransportControlsDisplayUpdater : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_Type(MediaPlaybackType* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_Type(MediaPlaybackType value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_AppMediaId(HSTRING* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_AppMediaId(HSTRING value) = 0;
    // The thumbnail is an IRandomAccessStreamReference.
    virtual HRESULT STDMETHODCALLTYPE get_Thumbnail(IInspectable** value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_Thumbnail(IInspectable* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_MusicProperties(IMusicDisplayProperties** value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_VideoProperties(IInspectable** value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_ImageProperties(IInspectable** value) = 0;
    virtual HRESULT STDMETHODCALLTYPE CopyFromFileAsync(MediaPlaybackType type, IInspectable* source,
                                                        IInspectable** operation) = 0;
    virtual HRESULT STDMETHODCALLTYPE ClearAll() = 0;
    virtual HRESULT STDMETHODCALLTYPE Update() = 0;
};

// Windows.Media.IMusicDisplayProperties
struct IMusicDisplayProperties : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_Title(HSTRING* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_Title(HSTRING value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_AlbumArtist(HSTRING* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_AlbumArtist(HSTRING value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_Artist(HSTRING* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_Artist(HSTRING value) = 0;
};

// Windows.Media.IMusicDisplayProperties2
constexpr IID kIID_IMusicDisplayProperties2 = {
    0x00368462, 0x97d3, 0x44b9, {0xb0, 0x0f, 0x00, 0x8a, 0xfc, 0xef, 0xaf, 0x18}};
struct IMusicDisplayProperties2 : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_AlbumTitle(HSTRING* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_AlbumTitle(HSTRING value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_TrackNumber(UINT32* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_TrackNumber(UINT32 value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_Genres(IInspectable** value) = 0;
};

// Windows.Media.ISystemMediaTransportControlsButtonPressedEventArgs
struct ISystemMediaTransportControlsButtonPressedEventArgs : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_Button(SystemMediaTransportControlsButton* value) = 0;
};

// Windows.Media.IPlaybackPositionChangeRequestedEventArgs
struct IPlaybackPositionChangeRequestedEventArgs : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_RequestedPlaybackPosition(TimeSpan* value) = 0;
};

// Windows.Media.ISystemMediaTransportControlsTimelineProperties
constexpr IID kIID_ISystemMediaTransportControlsTimelineProperties = {
    0x5125316a, 0xc3a2, 0x475b, {0x85, 0x07, 0x93, 0x53, 0x4d, 0xc8, 0x8f, 0x15}};
struct ISystemMediaTransportControlsTimelineProperties : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE get_StartTime(TimeSpan* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_StartTime(TimeSpan value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_EndTime(TimeSpan* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_EndTime(TimeSpan value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_MinSeekTime(TimeSpan* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_MinSeekTime(TimeSpan value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_MaxSeekTime(TimeSpan* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_MaxSeekTime(TimeSpan value) = 0;
    virtual HRESULT STDMETHODCALLTYPE get_Position(TimeSpan* value) = 0;
    virtual HRESULT STDMETHODCALLTYPE put_Position(TimeSpan value) = 0;
};

// Windows.Foundation.IUriRuntimeClassFactory. The Uri it creates is passed on
// opaquely.
constexpr IID kIID_IUriRuntimeClassFactory = {
    0x44a9796f, 0x723e, 0x4fdf, {0xa2, 0x18, 0x03, 0x3e, 0x75, 0xb0, 0xc0, 0x84}};
struct IUriRuntimeClassFactory : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE CreateUri(HSTRING uri, IInspectable** instance) = 0;
    virtual HRESULT STDMETHODCALLTYPE CreateWithRelativeUri(HSTRING baseUri, HSTRING relativeUri,
                                                            IInspectable** instance) = 0;
};

// Windows.Storage.Streams.IRandomAccessStreamReferenceStatics
constexpr IID kIID_IRandomAccessStreamReferenceStatics = {
    0x857309dc, 0x3fbf, 0x4e7d, {0x98, 0x6f, 0xef, 0x3b, 0x1a, 0x07, 0xa9, 0x64}};
struct IRandomAccessStreamReferenceStatics : IInspectable {
    virtual HRESULT STDMETHODCALLTYPE CreateFromFile(IInspectable* file, IInspectable** streamReference) = 0;
    virtual HRESULT STDMETHODCALLTYPE CreateFromUri(IInspectable* uri, IInspectable** streamReference) = 0;
    virtual HRESULT STDMETHODCALLTYPE CreateFromStream(IInspectable* stream, IInspectable** streamReference) = 0;
};

constexpr wchar_t kClassSystemMediaTransportControls[] = L"Windows.Media.SystemMediaTransportControls";
constexpr wchar_t kClassTimelineProperties[] = L"Windows.Media.SystemMediaTransportControlsTimelineProperties";
constexpr wchar_t kClassUri[] = L"Windows.Foundation.Uri";
constexpr wchar_t kClassRandomAccessStreamReference[] = L"Windows.Storage.Streams.RandomAccessStreamReference";

} // namespace vibez_winrt

#endif // VIBEZ_SMTC_WINRT_ABI_H
