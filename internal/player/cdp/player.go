//go:build linux

package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/web"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Player drives Apple Music playback through a Chrome DevTools Protocol session.
// Chrome's built-in Widevine CDM handles DRM; audio is output directly by
// Chrome to PulseAudio/PipeWire — GStreamer is not used.
//
// The absence of the goStreamURL binding signals musickit.html to use
// MusicKit's native m.play()/m.setQueue() path (Chrome/Widevine mode).
type Player struct {
	// OnUserToken is called when MusicKit JS reports a new or refreshed user token.
	OnUserToken func(token string)
	// OnStorefront is called when MusicKit JS detects the user's storefront.
	OnStorefront func(sf string)

	chromeCtx    context.Context
	cancelChrome context.CancelFunc

	srv *http.Server

	mu    sync.RWMutex
	state player.State
	subs  []chan player.State

	readyCh chan struct{} // closed when goAuthComplete fires
	doneCh  chan struct{} // closed by Terminate()
}

// promiseShim wraps each JS→Go binding to return a resolved Promise.
// Chrome's Runtime.addBinding creates synchronous functions that return
// undefined; MusicKit JS calls .catch() on every binding, which would throw
// TypeError without this shim.
const promiseShim = `(function(){
  var names=['goNeedsAuth','goAuthComplete','goPlayerStateChange',
              'goUserTokenChanged','goStorefrontChanged','goError'];
  names.forEach(function(n){
    var orig=window[n];
    if(typeof orig!=='function')return;
    window[n]=function(){
      var r;try{r=orig.apply(this,arguments);}catch(e){return Promise.resolve();}
      return(r&&typeof r.then==='function')?r:Promise.resolve(r);
    };
  });
})();`

// New creates a CDP Player. It probes for Chrome, renders the MusicKit page,
// starts a local HTTP server, and launches Chrome. Returns an error if Chrome
// is not installed.
func New(devToken, userToken, storefront string) (*Player, error) {
	chromePath, err := findChrome()
	if err != nil {
		return nil, err
	}

	html, err := web.RenderHTML(devToken, userToken, storefront, "1.0.0")
	if err != nil {
		return nil, fmt.Errorf("cdp: render html: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("cdp: listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})
	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()

	p := &Player{
		srv:     srv,
		readyCh: make(chan struct{}),
		doneCh:  make(chan struct{}),
	}

	// Build Chrome launch options.
	headless := userToken != "" // headless once we have a saved token
	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("enable-features", "MediaCapabilities"),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoFirstRun,
	)
	if headless {
		// headless=new (Chrome 112+) retains the full media stack including Widevine.
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else {
		// Visible window for first-run Apple Music auth.
		opts = append(opts, chromedp.WindowSize(640, 520))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	chromeCtx, cancelChrome := chromedp.NewContext(allocCtx)
	p.chromeCtx = chromeCtx
	p.cancelChrome = func() {
		cancelChrome()
		cancelAlloc()
	}

	// Register JS→Go bindings. goStreamURL is intentionally omitted — its
	// absence tells musickit.html to use Chrome's native Widevine playback path.
	bindNames := []string{
		"goNeedsAuth", "goAuthComplete", "goPlayerStateChange",
		"goUserTokenChanged", "goStorefrontChanged", "goError",
	}
	var bindActions chromedp.Tasks
	for _, name := range bindNames {
		bindActions = append(bindActions, runtime.AddBinding(name))
	}

	// Inject the Promise shim before the page's own scripts run.
	bindActions = append(bindActions, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(promiseShim).Do(ctx)
		return err
	}))

	if err := chromedp.Run(chromeCtx, bindActions); err != nil {
		p.cancelChrome()
		_ = srv.Close()
		return nil, fmt.Errorf("cdp: setup bindings: %w", err)
	}

	// Receive JS→Go binding events on a background goroutine.
	chromedp.ListenTarget(chromeCtx, func(ev any) {
		e, ok := ev.(*runtime.EventBindingCalled)
		if !ok {
			return
		}
		p.handleBinding(e.Name, e.Payload)
	})

	// Navigate async so New() returns quickly; caller calls WaitReady separately.
	srvURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	go func() {
		if err := chromedp.Run(chromeCtx, chromedp.Navigate(srvURL)); err != nil {
			select {
			case <-chromeCtx.Done():
			default:
				p.broadcastError(fmt.Sprintf("cdp navigate: %v", err))
			}
		}
	}()

	return p, nil
}

// handleBinding processes a JS→Go call from the Chrome page.
func (p *Player) handleBinding(name, payload string) {
	// Chrome encodes the binding argument as a JSON array: e.g. ["value"]
	arg := ""
	var args []json.RawMessage
	if json.Unmarshal([]byte(payload), &args) == nil && len(args) > 0 {
		_ = json.Unmarshal(args[0], &arg)
	}

	switch name {
	case "goNeedsAuth":
	// Nothing extra needed; Chrome shows a visible window for first-run auth.

	case "goAuthComplete":
		select {
		case <-p.readyCh:
		default:
			close(p.readyCh)
		}

	case "goPlayerStateChange":
		var js jsState
		if json.Unmarshal([]byte(arg), &js) == nil {
			p.applyState(js)
		}

	case "goUserTokenChanged":
		if p.OnUserToken != nil && arg != "" {
			p.OnUserToken(arg)
		}

	case "goStorefrontChanged":
		if p.OnStorefront != nil && arg != "" {
			p.OnStorefront(arg)
		}

	case "goError":
		p.broadcastError(arg)
	}
}

func (p *Player) broadcastError(msg string) {
	p.mu.Lock()
	p.state.Error = msg
	s := p.state
	subs := p.subs
	p.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- s:
		default:
		}
	}
}

// jsState mirrors the JSON payload sent by goPlayerStateChange.
type jsState struct {
	IsPlaying   bool     `json:"isPlaying"`
	CurrentTime float64  `json:"currentTime"`
	Duration    float64  `json:"duration"`
	Volume      float64  `json:"volume"`
	NowPlaying  *jsTrack `json:"nowPlaying"`
}

type jsTrack struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	ArtworkURL string `json:"artworkURL"`
	DurationMs int64  `json:"durationMs"`
}

func (p *Player) applyState(js jsState) {
	s := player.State{
		Playing:  js.IsPlaying,
		Position: time.Duration(js.CurrentTime * float64(time.Second)),
		Volume:   js.Volume,
	}
	if js.NowPlaying != nil {
		s.Track = &provider.Track{
			ID:         js.NowPlaying.ID,
			Title:      js.NowPlaying.Title,
			Artist:     js.NowPlaying.Artist,
			Album:      js.NowPlaying.Album,
			ArtworkURL: js.NowPlaying.ArtworkURL,
			Duration:   time.Duration(js.NowPlaying.DurationMs) * time.Millisecond,
		}
	}
	p.mu.Lock()
	p.state = s
	subs := p.subs
	p.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- s:
		default:
		}
	}
}

// dispatch evaluates JavaScript in Chrome asynchronously.
func (p *Player) dispatch(js string) {
	go func() {
		_ = chromedp.Run(p.chromeCtx, chromedp.Evaluate(js, nil))
	}()
}

// ── Lifecycle ─────────────────────────────────────────────────────────────

// Run blocks until Terminate() is called, mirroring webkit.Player.Run().
func (p *Player) Run() {
	<-p.doneCh
	p.cancelChrome()
	_ = p.srv.Close()
}

// Terminate stops Chrome and unblocks Run(). Safe to call multiple times.
func (p *Player) Terminate() {
	select {
	case <-p.doneCh:
	default:
		close(p.doneCh)
	}
}

// WaitReady blocks until MusicKit JS completes authorization, or ctx is cancelled.
func (p *Player) WaitReady(ctx context.Context) error {
	select {
	case <-p.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cdp player: %w", ctx.Err())
	}
}

// ── player.Player interface ───────────────────────────────────────────────

// Play resumes playback.
func (p *Player) Play() error {
	p.dispatch(`window.vibezPlay && window.vibezPlay()`)
	return nil
}

// Pause pauses playback.
func (p *Player) Pause() error {
	p.dispatch(`window.vibezPause && window.vibezPause()`)
	return nil
}

// Stop halts playback.
func (p *Player) Stop() error {
	p.dispatch(`window.vibezPause && window.vibezPause()`)
	return nil
}

// Next skips to the next track.
func (p *Player) Next() error {
	p.dispatch(`window.vibezNext && window.vibezNext()`)
	return nil
}

// Previous skips to the previous track.
func (p *Player) Previous() error {
	p.dispatch(`window.vibezPrev && window.vibezPrev()`)
	return nil
}

// Seek seeks to position.
func (p *Player) Seek(position time.Duration) error {
	p.dispatch(fmt.Sprintf(`window.vibezSeek && window.vibezSeek(%f)`, position.Seconds()))
	return nil
}

// SetVolume sets the volume (0.0–1.0).
func (p *Player) SetVolume(v float64) error {
	p.dispatch(fmt.Sprintf(`window.vibezSetVolume && window.vibezSetVolume(%f)`, v))
	return nil
}

// SetQueue replaces the playback queue with the given song IDs and starts playback.
func (p *Player) SetQueue(ids []string) error {
	b, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("cdp: marshal queue ids: %w", err)
	}
	js, _ := json.Marshal(string(b))
	p.dispatch(fmt.Sprintf(`window.vibezSetQueue && window.vibezSetQueue(%s)`, js))
	return nil
}

// SetPlaylist queues a library playlist by ID and starts from startIdx.
func (p *Player) SetPlaylist(playlistID string, startIdx int) error {
	js, _ := json.Marshal(playlistID)
	p.dispatch(fmt.Sprintf(`window.vibezSetPlaylist && window.vibezSetPlaylist(%s,%d)`, js, startIdx))
	return nil
}

// GetState returns a snapshot of the current playback state.
func (p *Player) GetState() (*player.State, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.state
	return &s, nil
}

// Subscribe returns a channel that receives state updates.
func (p *Player) Subscribe() <-chan player.State {
	ch := make(chan player.State, 8)
	p.mu.Lock()
	p.subs = append(p.subs, ch)
	p.mu.Unlock()
	return ch
}

// Close releases all Chrome and HTTP server resources.
func (p *Player) Close() error {
	p.Terminate()
	return nil
}
