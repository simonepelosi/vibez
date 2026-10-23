//go:build windows && cgo

package smtc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// readerScript looks the session up from the reader side, the way YASB and the
// volume flyout do: through GlobalSystemMediaTransportControlsSessionManager.
// MinGW has no windows.media.control.h, so Windows PowerShell 5.1, which can
// project WinRT types, does the reading.
//
// It finds the session whose title is $env:SMTC_TITLE, waiting up to
// $env:SMTC_WAIT seconds for it to appear with status $env:SMTC_STATUS (any
// status if empty), or with $env:SMTC_ABSENT=1 for it to disappear. It then
// runs $env:SMTC_ACTION on that session only and prints one JSON object.
// It never touches any other app's session beyond reading its title.
const readerScript = `
$ErrorActionPreference = 'Stop'
function Out-Result($r) { $r | ConvertTo-Json -Compress; exit 0 }
try {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and
        $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1'
    } | Select-Object -First 1
    function Await($op, [Type]$type) {
        $task = $asTask.MakeGenericMethod($type).Invoke($null, @($op))
        if (-not $task.Wait(10000)) { throw 'WinRT call timed out' }
        $task.Result
    }
    $mgrType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType = WindowsRuntime]
    $propsType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties, Windows.Media.Control, ContentType = WindowsRuntime]
    $mgr = Await ($mgrType::RequestAsync()) $mgrType
} catch {
    Out-Result ([ordered]@{ reader = 'unavailable'; error = $_.Exception.Message })
}

function Find-Session {
    foreach ($s in $mgr.GetSessions()) {
        try { $p = Await ($s.TryGetMediaPropertiesAsync()) $propsType } catch { continue }
        if ($p.Title -eq $env:SMTC_TITLE) { return @($s, $p) }
    }
    return $null
}

$deadline = (Get-Date).AddSeconds([double]$env:SMTC_WAIT)
while ($true) {
    $hit = Find-Session
    if ($env:SMTC_ABSENT -eq '1') {
        if (-not $hit) { Out-Result ([ordered]@{ reader = 'ok'; found = $false }) }
    } elseif ($hit) {
        $status = $hit[0].GetPlaybackInfo().PlaybackStatus.ToString()
        if (-not $env:SMTC_STATUS -or $status -eq $env:SMTC_STATUS) { break }
    }
    if ((Get-Date) -gt $deadline) { break }
    Start-Sleep -Milliseconds 200
}
if (-not $hit) { Out-Result ([ordered]@{ reader = 'ok'; found = $false }) }

$session, $props = $hit
$action = $null
switch ($env:SMTC_ACTION) {
    'next'   { $action = Await ($session.TrySkipNextAsync()) ([bool]) }
    'prev'   { $action = Await ($session.TrySkipPreviousAsync()) ([bool]) }
    'toggle' { $action = Await ($session.TryTogglePlayPauseAsync()) ([bool]) }
    'seek'   { $action = Await ($session.TryChangePlaybackPositionAsync([long]$env:SMTC_SEEK_TICKS)) ([bool]) }
}
$thumbBytes = -1
if ($props.Thumbnail) {
    try {
        $streamType = [Windows.Storage.Streams.IRandomAccessStreamWithContentType, Windows.Storage.Streams, ContentType = WindowsRuntime]
        $stream = Await ($props.Thumbnail.OpenReadAsync()) $streamType
        # PowerShell 5.1 does not see members a WinRT interface inherits, so
        # Size and ContentType are read through their declaring interfaces.
        $rasType = [Windows.Storage.Streams.IRandomAccessStream, Windows.Storage.Streams, ContentType = WindowsRuntime]
        $ctpType = [Windows.Storage.Streams.IContentTypeProvider, Windows.Storage.Streams, ContentType = WindowsRuntime]
        $thumbBytes = $rasType.GetProperty('Size').GetValue($stream)
        $thumbType = $ctpType.GetProperty('ContentType').GetValue($stream)
    } catch { $thumbBytes = -2; $thumbErr = $_.Exception.ToString() }
}
$info = $session.GetPlaybackInfo()
$tl = $session.GetTimelineProperties()
Out-Result ([ordered]@{
    reader          = 'ok'
    found           = $true
    aumid           = $session.SourceAppUserModelId
    title           = $props.Title
    artist          = $props.Artist
    album           = $props.AlbumTitle
    hasThumbnail    = ($null -ne $props.Thumbnail)
    thumbnailBytes  = $thumbBytes
    thumbnailError  = $thumbErr
    thumbnailType   = $thumbType
    playbackType    = "$($info.PlaybackType)"
    status          = $info.PlaybackStatus.ToString()
    playEnabled     = $info.Controls.IsPlayEnabled
    pauseEnabled    = $info.Controls.IsPauseEnabled
    nextEnabled     = $info.Controls.IsNextEnabled
    previousEnabled = $info.Controls.IsPreviousEnabled
    seekEnabled     = $info.Controls.IsPlaybackPositionEnabled
    positionMs      = $tl.Position.TotalMilliseconds
    endMs           = $tl.EndTime.TotalMilliseconds
    action          = $action
})
`

type readerResult struct {
	Reader          string  `json:"reader"`
	Error           string  `json:"error"`
	Found           bool    `json:"found"`
	AUMID           string  `json:"aumid"`
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	Album           string  `json:"album"`
	HasThumbnail    bool    `json:"hasThumbnail"`
	ThumbnailBytes  int64   `json:"thumbnailBytes"`
	ThumbnailType   string  `json:"thumbnailType"`
	ThumbnailError  string  `json:"thumbnailError"`
	PlaybackType    string  `json:"playbackType"`
	Status          string  `json:"status"`
	PlayEnabled     bool    `json:"playEnabled"`
	PauseEnabled    bool    `json:"pauseEnabled"`
	NextEnabled     bool    `json:"nextEnabled"`
	PreviousEnabled bool    `json:"previousEnabled"`
	SeekEnabled     bool    `json:"seekEnabled"`
	PositionMs      float64 `json:"positionMs"`
	EndMs           float64 `json:"endMs"`
	Action          *bool   `json:"action"`
}

type readerQuery struct {
	title     string
	status    string // wait for this status; "" = any
	absent    bool   // wait for the session to disappear instead
	action    string // "", next, prev, toggle, seek
	seekTicks int64
}

// readSMTC runs the reader script. It skips the test when the reader side
// does not exist on this machine.
func readSMTC(t *testing.T, q readerQuery) readerResult {
	t.Helper()
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skipf("Windows PowerShell not found, cannot read SMTC: %v", err)
	}
	script := filepath.Join(t.TempDir(), "smtc-reader.ps1")
	if err := os.WriteFile(script, []byte(readerScript), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script) //nolint:gosec // G204: Windows PowerShell running the test's own script
	absent := "0"
	if q.absent {
		absent = "1"
	}
	cmd.Env = append(os.Environ(),
		"SMTC_TITLE="+q.title,
		"SMTC_STATUS="+q.status,
		"SMTC_ABSENT="+absent,
		"SMTC_WAIT=10",
		"SMTC_ACTION="+q.action,
		fmt.Sprintf("SMTC_SEEK_TICKS=%d", q.seekTicks),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("reader script: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if i := strings.LastIndex(out, "\n"); i >= 0 {
		out = strings.TrimSpace(out[i+1:])
	}
	var r readerResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("reader output %q: %v\nstderr: %s", stdout.String(), err, stderr.String())
	}
	if r.Reader != "ok" {
		t.Skipf("SMTC reader unavailable: %s", r.Error)
	}
	t.Logf("reader: %s", out)
	return r
}

// requireFound fails the test, or skips it on CI where the runner may have no
// interactive shell to host media sessions, when the session is not visible.
func requireFound(t *testing.T, r readerResult, what string) {
	t.Helper()
	if r.Found {
		return
	}
	if os.Getenv("CI") != "" {
		t.Skipf("%s: the session never reached the SMTC reader; this runner may not host media sessions", what)
	}
	t.Fatalf("%s: session not visible to GlobalSystemMediaTransportControlsSessionManager", what)
}

// artworkServer serves a small PNG on loopback, so the thumbnail path is
// exercised without the network. It counts requests: Windows fetches the
// image itself when the display is updated.
func artworkServer(t *testing.T) (url string, body []byte, hits *atomic.Int32) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for x := range 32 {
		for y := range 32 {
			img.Set(x, y, color.RGBA{R: 220, G: 40, B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	hits = &atomic.Int32{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(ts.Close)
	return ts.URL + "/art/300x300.png", buf.Bytes(), hits
}

// TestReaderSeesSessionAndDrivesController creates a real SMTC session and
// checks it from the reader side: the metadata, artwork, status and timeline
// readers show, and that a reader's transport requests reach the Controller.
// It presses nothing globally, so audio playing in another app is not
// affected.
func TestReaderSeesSessionAndDrivesController(t *testing.T) {
	ctrl := newFakeController()
	srv, err := NewServer(ctrl, func(msg string) { t.Log(msg) })
	if err != nil {
		t.Skipf("SMTC unavailable on this machine: %v", err)
	}
	defer func() { _ = srv.Close() }()

	artURL, artBody, artHits := artworkServer(t)
	title := fmt.Sprintf("vibez SMTC test %d", time.Now().UnixNano())
	track := &provider.Track{
		ID:         "smtc-test",
		Title:      title,
		Artist:     "vibez test artist",
		Album:      "vibez test album",
		Duration:   3 * time.Minute,
		ArtworkURL: artURL,
	}
	srv.Update(player.State{Track: track, Playing: true, Position: 42 * time.Second})

	// Windows fetches the artwork when the display is updated. Wait for that
	// request so the reader below cannot race it.
	for deadline := time.Now().Add(5 * time.Second); artHits.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	r := readSMTC(t, readerQuery{title: title, status: "Playing"})
	requireFound(t, r, "after the first Update")
	t.Logf("SourceAppUserModelId as readers see it: %q", r.AUMID)
	if r.Artist != track.Artist || r.Album != track.Album {
		t.Errorf("reader sees artist %q album %q, want %q %q", r.Artist, r.Album, track.Artist, track.Album)
	}
	if r.Status != "Playing" {
		t.Errorf("reader sees status %q, want Playing", r.Status)
	}
	if r.PlaybackType != "Music" {
		t.Errorf("reader sees playback type %q, want Music", r.PlaybackType)
	}
	if !r.HasThumbnail || r.ThumbnailBytes != int64(len(artBody)) || r.ThumbnailType != "image/png" {
		t.Errorf("reader sees thumbnail %v: %d bytes of %q (%s), want the %d-byte PNG",
			r.HasThumbnail, r.ThumbnailBytes, r.ThumbnailType, r.ThumbnailError, len(artBody))
	}
	if artHits.Load() == 0 {
		t.Error("Windows never fetched the artwork URL")
	}
	// Readers derive the enabled buttons from the status as well: while
	// playing, Play is reported disabled and Pause enabled.
	if !r.PauseEnabled || !r.NextEnabled || !r.PreviousEnabled || !r.SeekEnabled {
		t.Errorf("controls while playing: pause %v next %v previous %v seek %v, want all enabled",
			r.PauseEnabled, r.NextEnabled, r.PreviousEnabled, r.SeekEnabled)
	}
	if r.EndMs != float64(track.Duration.Milliseconds()) {
		t.Errorf("reader sees timeline end %v ms, want %v", r.EndMs, track.Duration.Milliseconds())
	}
	if r.PositionMs != 42000 {
		t.Errorf("reader sees position %v ms, want 42000", r.PositionMs)
	}

	r = readSMTC(t, readerQuery{title: title, action: "next"})
	requireFound(t, r, "before TrySkipNextAsync")
	if r.Action == nil || !*r.Action {
		t.Errorf("TrySkipNextAsync returned %v, want true", r.Action)
	}
	if got := ctrl.waitCall(t, 5*time.Second); got != "next" {
		t.Errorf("TrySkipNextAsync reached the controller as %q, want next", got)
	}

	r = readSMTC(t, readerQuery{title: title, action: "prev"})
	requireFound(t, r, "before TrySkipPreviousAsync")
	if got := ctrl.waitCall(t, 5*time.Second); got != "previous" {
		t.Errorf("TrySkipPreviousAsync reached the controller as %q, want previous", got)
	}

	// Toggling while Playing must ask for Pause.
	r = readSMTC(t, readerQuery{title: title, action: "toggle"})
	requireFound(t, r, "before TryTogglePlayPauseAsync")
	if got := ctrl.waitCall(t, 5*time.Second); got != "pause" {
		t.Errorf("toggle while playing reached the controller as %q, want pause", got)
	}

	// Once the player reports the pause, readers see it and a toggle asks
	// for Play.
	srv.Update(player.State{Track: track, Position: 43 * time.Second})
	r = readSMTC(t, readerQuery{title: title, status: "Paused", action: "toggle"})
	requireFound(t, r, "after pausing")
	if r.Status != "Paused" || !r.PlayEnabled {
		t.Errorf("reader sees status %q, play enabled %v after pause; want Paused, true", r.Status, r.PlayEnabled)
	}
	if r.PositionMs != 43000 {
		t.Errorf("reader sees position %v ms after pause, want 43000", r.PositionMs)
	}
	if got := ctrl.waitCall(t, 5*time.Second); got != "play" {
		t.Errorf("toggle while paused reached the controller as %q, want play", got)
	}

	ticks := (90 * time.Second).Nanoseconds() / 100
	r = readSMTC(t, readerQuery{title: title, action: "seek", seekTicks: ticks})
	requireFound(t, r, "before TryChangePlaybackPositionAsync")
	if got := ctrl.waitCall(t, 5*time.Second); got != "seek 1m30s" {
		t.Errorf("TryChangePlaybackPositionAsync reached the controller as %q, want seek 1m30s", got)
	}

	// A track with no artwork must not keep the previous track's thumbnail.
	second := &provider.Track{ID: "smtc-test-2", Title: title + " (2)", Artist: "second artist", Duration: time.Minute}
	srv.Update(player.State{Track: second, Playing: true})
	r = readSMTC(t, readerQuery{title: second.Title, status: "Playing"})
	requireFound(t, r, "after a track change")
	if r.Artist != second.Artist || r.Album != "" || r.HasThumbnail {
		t.Errorf("after a track change reader sees artist %q album %q thumbnail %v; want %q, empty, none",
			r.Artist, r.Album, r.HasThumbnail, second.Artist)
	}
	if r.EndMs != 60000 || r.PositionMs != 0 {
		t.Errorf("after a track change reader sees %v of %v ms, want 0 of 60000", r.PositionMs, r.EndMs)
	}

	// No track: the session leaves the system UI.
	srv.Update(player.State{})
	r = readSMTC(t, readerQuery{title: second.Title, absent: true})
	if r.Found {
		t.Error("session still visible with no track")
	}

	// Close takes the session out of the system UI as well.
	srv.Update(player.State{Track: second, Playing: true})
	r = readSMTC(t, readerQuery{title: second.Title, status: "Playing"})
	requireFound(t, r, "after the track returned")
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	r = readSMTC(t, readerQuery{title: second.Title, absent: true})
	if r.Found {
		t.Error("session still visible after Close")
	}
}

// TestNativeBridgeLifecycle exercises the native thread, window and handlers
// without a reader: create, update with every field group, clear, destroy.
func TestNativeBridgeLifecycle(t *testing.T) {
	for i := range 3 {
		b, err := newNativeBridge(func(event) {}, func(msg string) { t.Errorf("native error: %s", msg) })
		if err != nil {
			t.Skipf("SMTC unavailable on this machine: %v", err)
		}
		st := statusPlaying
		b.apply(update{
			Metadata: &metadata{Title: fmt.Sprintf("lifecycle %d", i), Artist: "ünïcödé ✓", Album: "a\x00b", ArtworkURL: "not a url"},
			Status:   &st,
			Timeline: &timeline{Position: time.Second, Duration: time.Minute},
		})
		closed := statusClosed
		b.apply(update{Status: &closed})
		b.close()
		b.close() // idempotent
	}
}
