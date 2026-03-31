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
	"strings"
	"sync"
	"text/template"
	"time"

	_ "embed"

	webview "github.com/webview/webview_go"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

//go:embed web/musickit.html
var musickitHTML string

// jsState mirrors the JSON object sent by the MusicKit JS notifyState function.
type jsState struct {
	IsPlaying     bool     `json:"isPlaying"`
	PlaybackState int      `json:"playbackState"`
	CurrentTime   float64  `json:"currentTime"`
	Duration      float64  `json:"duration"`
	Volume        float64  `json:"volume"`
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
	mu      sync.RWMutex
	state   player.State
	subs    []chan player.State
	readyCh chan struct{}
	errCh   chan error

	// OnUserToken is called when MusicKit JS reports a new or updated user token.
	OnUserToken func(token string)
}

// New creates a Player and loads MusicKit JS into a fully hidden WebView.
// Call Run() on the main OS goroutine to start the GTK event loop.
func New(devToken, userToken string) (*Player, error) {
	html, err := renderHTML(devToken, userToken)
	if err != nil {
		return nil, err
	}

	// Force GTK to use the X11 (XWayland) backend on Wayland sessions.
	// The native Wayland backend (GDK_BACKEND=wayland) does not support
	// off-screen window management: moving a window outside the screen is a
	// Wayland protocol violation that causes compositor crashes and monitor
	// flicker. XWayland handles this correctly.
	// Also disable WebKit's GPU process before GTK init — WebKit reads this
	// at startup and the env vars must be set before webview.New().
	if os.Getenv("GDK_BACKEND") == "" {
		_ = os.Setenv("GDK_BACKEND", "x11")
	}
	_ = os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	// JSC uses SIGUSR1 (10) for its GC by default, which conflicts with Go's
	// signal handling. Redirect it to SIGRTMIN+4 (38), a real-time signal
	// unused by Go's runtime and not reserved by the OS on Linux.
	if os.Getenv("JSC_SIGNAL_FOR_GC") == "" {
		_ = os.Setenv("JSC_SIGNAL_FOR_GC", "38")
	}

	p := &Player{
		readyCh: make(chan struct{}),
		errCh:   make(chan error, 1),
	}

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
	// Disable WebKit's "user gesture required" autoplay policy so MusicKit JS
	// can call music.play() programmatically after setting the queue.
	// Scheduled via Dispatch so it runs once the GTK loop is up.
	w.Dispatch(func() { allowAutoplay(w.Window()) })

	if err := bindAll(w, p); err != nil {
		w.Destroy()
		return nil, err
	}

	w.SetHtml(html)
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
func (p *Player) Terminate() {
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

func (p *Player) Play() error {
	p.dispatch(`window.vibezPlay && window.vibezPlay()`)
	return nil
}

func (p *Player) Pause() error {
	p.dispatch(`window.vibezPause && window.vibezPause()`)
	return nil
}

func (p *Player) Stop() error {
	p.dispatch(`window.vibezPause && window.vibezPause()`)
	return nil
}

func (p *Player) Next() error {
	p.dispatch(`window.vibezNext && window.vibezNext()`)
	return nil
}

func (p *Player) Previous() error {
	p.dispatch(`window.vibezPrev && window.vibezPrev()`)
	return nil
}

func (p *Player) Seek(position time.Duration) error {
	secs := position.Seconds()
	p.dispatch(fmt.Sprintf(`window.vibezSeek && window.vibezSeek(%f)`, secs))
	return nil
}

func (p *Player) SetVolume(v float64) error {
	p.dispatch(fmt.Sprintf(`window.vibezSetVolume && window.vibezSetVolume(%f)`, v))
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

func renderHTML(devToken, userToken string) (string, error) {
	tmpl, err := template.New("musickit").Parse(musickitHTML)
	if err != nil {
		return "", fmt.Errorf("parsing musickit template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, map[string]string{
		"DeveloperToken": devToken,
		"UserToken":      userToken,
		"Version":        "1.0.0",
	}); err != nil {
		return "", fmt.Errorf("rendering musickit template: %w", err)
	}
	return buf.String(), nil
}

// jsonStringLiteral wraps a JSON string in a JS string literal so it can be
// passed safely as an argument to a JS function via Eval.
func jsonStringLiteral(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
