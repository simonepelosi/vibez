// Package webkit provides an Apple Music player backed by an embedded
// WebKit2GTK WebView running MusicKit JS. This eliminates the dependency on
// Cider or any external MPRIS player — vibez owns the full playback stack.
//
// Threading model:
//
//	Main OS goroutine → w.Run()  (GTK event loop — must never block)
//	Any other goroutine          → w.Dispatch(fn) to schedule GTK work
//	BubbleTea goroutine          → reads from Subscribe() channel
package webkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	webview "github.com/webview/webview_go"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/gst"
	"github.com/simone-vibes/vibez/internal/player/web"
	"github.com/simone-vibes/vibez/internal/provider"
)

// jsState
type jsState struct {
	IsPlaying     bool     `json:"isPlaying"`
	PlaybackState int      `json:"playbackState"`
	CurrentTime   float64  `json:"currentTime"`
	Duration      float64  `json:"duration"`
	Volume        float64  `json:"volume"`
	RepeatMode    int      `json:"repeatMode"`
	ShuffleMode   int      `json:"shuffleMode"`
	NowPlaying    *jsTrack `json:"nowPlaying"`
}

type jsTrack struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	ArtworkURL string `json:"artworkURL"`
	DurationMs int64  `json:"durationMs"`
}

// Player implements player.Player using a hidden WebKit2GTK window.
type Player struct {
	w       webview.WebView
	gst     *gst.Player
	mu      sync.RWMutex
	state   player.State
	subs    []chan player.State
	readyCh chan struct{}
	errCh   chan error

	// OnUserToken is called when MusicKit JS reports a new or updated user token.
	OnUserToken func(token string)
	// OnStorefront is called when MusicKit JS reports the user's storefront after auth.
	OnStorefront func(storefront string)
}

// New creates a Player and loads MusicKit JS into a fully hidden WebView.
// Call Run() on the main OS goroutine to start the GTK event loop.
func New(devToken, userToken, storefront string) (*Player, error) {
	html, err := web.RenderHTML(devToken, userToken, storefront, "1.0.0")
	if err != nil {
		return nil, err
	}

	// Do NOT force GDK_BACKEND=x11. The window hiding strategy (opacity=0 +
	// gtk_widget_hide + UTILITY hint) works on both X11 and native Wayland
	// without any off-screen positioning. Forcing x11 breaks pure Wayland
	// sessions that don't have XWayland installed.
	//
	// WEBKIT_DISABLE_COMPOSITING_MODE=1 disables WebKit's GPU compositor for
	// web content rendering (independent of the EME/CDM stack). Without it,
	// WebKit's GPU process competes with the system compositor on every track
	// transition, freezing the entire desktop for 1-3 seconds. The EME/DRM
	// pipeline is driven by GStreamer (not the GPU compositor) so FairPlay key
	// exchange continues to work with compositing disabled.
	_ = os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	// Suppress Go's SIGUSR1 handler so JSC (WebKit's JS engine) can install
	// its own signal-10 handler for GC. signal.Ignore sets SIG_IGN, which
	// JSC can override. Using signal.Reset (SIG_DFL) was wrong: SIG_DFL for
	// SIGUSR1 terminates the process, so any signal-10 delivery during CGO
	// caused a SIGSEGV ("signal arrived during cgo execution").
	signal.Ignore(syscall.SIGUSR1)

	p := &Player{
		readyCh: make(chan struct{}),
		errCh:   make(chan error, 1),
	}

	gstPlayer, err := gst.New()
	if err != nil {
		return nil, fmt.Errorf("webkit player: %w", err)
	}
	p.gst = gstPlayer
	p.gst.OnEOS(func() {
		// When GStreamer finishes a track, advance to the next one in the JS queue.
		p.dispatch(`window.vibezNext && window.vibezNext()`)
	})
	p.gst.OnError(func(e error) {
		p.mu.Lock()
		s := p.state
		s.Error = e.Error()
		p.mu.Unlock()
		for _, ch := range p.subs {
			select {
			case ch <- s:
			default:
			}
		}
	})

	w := webview.New(false)
	w.SetTitle("vibez-audio")
	w.SetSize(1, 1, webview.HintFixed)

	// Hide the window before the GTK main loop gets a chance to render it.
	// webview.New() calls gtk_widget_show_all() internally, so we counter it
	// immediately — before Run() iterates the event loop.
	hideGTKWindow(w.Window())
	// Connect a "map" signal callback that re-hides the window whenever GTK
	// tries to show it while opacity is 0. This eliminates the startup flash.
	connectHideOnMap(w.Window())
	// Apply WebKit settings (EME, MSE, autoplay, hardware acceleration)
	// SYNCHRONOUSLY before loading any content. If these are deferred via
	// Dispatch(), the WebContent process is already initialised with default
	// settings by the time they run, and the changes have no effect.
	// We're still on the main OS goroutine here, so direct GTK calls are safe.
	allowAutoplay(w.Window())

	if err := bindAll(w, p); err != nil {
		w.Destroy()
		return nil, err
	}

	// Load the HTML with "http://localhost/" as the base URI so the page has
	// a "potentially trustworthy" secure context. EME (requestMediaKeySystemAccess)
	// is only available in secure contexts — SetHtml's null origin blocks it.
	loadHTMLLocalhost(w.Window(), html)
	p.w = w
	return p, nil
}

// Run starts the GTK main event loop. Must be called from the main OS goroutine.
// Blocks until Terminate() is called (e.g. when the TUI exits).
func (p *Player) Run() {
	p.w.Run()
	p.w.Destroy()
}

// Terminate signals GTK to stop. Safe to call from any goroutine.
// A recover guard protects against calling Terminate on a webview whose
// underlying GTK window was already destroyed by a signal or other crash.
func (p *Player) Terminate() {
	defer func() { recover() }() //nolint:errcheck
	p.w.Terminate()
}

// WaitReady blocks until MusicKit JS calls goReady, or until ctx is cancelled.
func (p *Player) WaitReady(ctx context.Context) error {
	select {
	case <-p.readyCh:
		return nil
	case err := <-p.errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("webkit player: %w", ctx.Err())
	}
}

// --- player.Player implementation ---

// Play resumes GStreamer and keeps MusicKit in sync.
func (p *Player) Play() error {
	p.gst.Play()
	p.dispatch(`window.vibezPlay && window.vibezPlay()`)
	return nil
}

// Pause pauses GStreamer and keeps MusicKit in sync.
func (p *Player) Pause() error {
	p.gst.Pause()
	p.dispatch(`window.vibezPause && window.vibezPause()`)
	return nil
}

// Stop halts GStreamer and MusicKit.
func (p *Player) Stop() error {
	p.gst.Stop()
	p.dispatch(`window.vibezPause && window.vibezPause()`)
	return nil
}

// Next advances the MusicKit queue; the audio interceptor fires the next URL to GStreamer.
func (p *Player) Next() error {
	p.dispatch(`window.vibezNext && window.vibezNext()`)
	return nil
}

// Previous goes back in the MusicKit queue.
func (p *Player) Previous() error {
	p.dispatch(`window.vibezPrev && window.vibezPrev()`)
	return nil
}

// Seek seeks GStreamer to position (MusicKit seek is skipped — WebView is muted).
func (p *Player) Seek(position time.Duration) error {
	p.gst.Seek(position)
	return nil
}

// SetVolume sets GStreamer output volume (WebView is muted; MusicKit volume has no effect).
func (p *Player) SetVolume(v float64) error {
	p.gst.SetVolume(v)
	return nil
}

func (p *Player) SetQueue(ids []string) error {
	b, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("marshalling queue ids: %w", err)
	}
	js := fmt.Sprintf(`window.vibezSetQueue && window.vibezSetQueue(%s)`, jsonStringLiteral(string(b)))
	p.dispatch(js)
	return nil
}

func (p *Player) SetPlaylist(playlistID string, startIdx int) error {
	js := fmt.Sprintf(`window.vibezSetPlaylist && window.vibezSetPlaylist(%s, %d)`,
		jsonStringLiteral(playlistID), startIdx)
	p.dispatch(js)
	return nil
}

func (p *Player) SetRepeat(mode int) error {
	p.dispatch(fmt.Sprintf(`window.vibezSetRepeat && window.vibezSetRepeat(%d)`, mode))
	return nil
}

func (p *Player) SetShuffle(on bool) error {
	v := 0
	if on {
		v = 1
	}
	p.dispatch(fmt.Sprintf(`window.vibezSetShuffle && window.vibezSetShuffle(%d)`, v))
	return nil
}

func (p *Player) GetState() (*player.State, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.state
	return &s, nil
}

func (p *Player) Subscribe() <-chan player.State {
	ch := make(chan player.State, 8)
	p.mu.Lock()
	p.subs = append(p.subs, ch)
	p.mu.Unlock()
	return ch
}

func (p *Player) Close() error {
	p.gst.Destroy()
	p.Terminate()
	return nil
}

// --- internal helpers ---

func (p *Player) dispatch(js string) {
	p.w.Dispatch(func() {
		p.w.Eval(js)
	})
}

func (p *Player) applyState(js jsState) {
	s := player.State{
		Playing:     js.IsPlaying,
		Position:    p.gst.Position(), // GStreamer is authoritative for position
		Volume:      js.Volume,
		RepeatMode:  js.RepeatMode,
		ShuffleMode: js.ShuffleMode != 0,
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

// bindAll registers all Go functions that MusicKit JS can call.
func bindAll(w webview.WebView, p *Player) error {
	bindings := map[string]any{
		"goNeedsAuth": func(_ string) {
			// MusicKit is about to show an auth dialog — make the window visible.
			p.w.Dispatch(func() {
				showGTKWindow(p.w.Window())
			})
		},
		"goAuthComplete": func(_ string) {
			// Auth finished — hide the window and unblock WaitReady.
			p.w.Dispatch(func() {
				hideGTKWindow(p.w.Window())
			})
			select {
			case <-p.readyCh: // already closed, ignore
			default:
				close(p.readyCh)
			}
		},
		"goPlayerStateChange": func(stateJSON string) {
			var js jsState
			if err := json.Unmarshal([]byte(stateJSON), &js); err != nil {
				return
			}
			p.applyState(js)
		},
		"goUserTokenChanged": func(token string) {
			if p.OnUserToken != nil && token != "" {
				p.OnUserToken(token)
			}
		},
		"goStorefrontChanged": func(sf string) {
			if p.OnStorefront != nil && sf != "" {
				p.OnStorefront(sf)
			}
		},
		// goStreamURL is called when MusicKit fires nowPlayingItemDidChange — we
		// extract the preview URL from the item's attributes in JS and forward it
		// here so GStreamer can play it natively (MusicKit uses MSE/blob URLs
		// internally that GStreamer cannot consume).
		"goStreamURL": func(url string) {
			p.gst.PlayURI(url)
		},
		"goError": func(msg string) {
			// Send to errCh for WaitReady (e.g. auth failure).
			select {
			case p.errCh <- fmt.Errorf("musickit: %s", msg):
			default:
			}
			// Also broadcast to state subscribers so the TUI displays the error.
			p.mu.Lock()
			s := p.state
			s.Error = msg
			p.mu.Unlock()
			for _, ch := range p.subs {
				select {
				case ch <- s:
				default:
				}
			}
		},
	}

	for name, fn := range bindings {
		if err := w.Bind(name, fn); err != nil {
			return fmt.Errorf("binding %s: %w", name, err)
		}
	}
	return nil
}

func jsonStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
