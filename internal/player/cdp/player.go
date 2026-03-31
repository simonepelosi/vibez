//go:build linux

package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	playwright "github.com/playwright-community/playwright-go"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/web"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Player drives Apple Music playback through a Playwright-managed Chrome
// browser stored in vibez's private cache. Chrome's built-in Widevine CDM
// enables full-track DRM playback. Audio goes directly to PulseAudio/PipeWire
// via Chrome — GStreamer is not used.
//
// The absence of the goStreamURL binding tells musickit.html to use Chrome's
// native m.play()/m.setQueue() Widevine path instead of GStreamer previews.
type Player struct {
	OnUserToken  func(token string)
	OnStorefront func(sf string)

	pw      *playwright.Playwright
	browser playwright.Browser
	page    playwright.Page
	srv     *http.Server

	mu    sync.RWMutex
	state player.State
	subs  []chan player.State

	readyCh chan struct{}
	errCh   chan error
	doneCh  chan struct{}
}

// New creates a CDP Player. EnsureBrowser must be called once before New().
func New(devToken, userToken, storefront string) (*Player, error) {
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
	srv := &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	p := &Player{
		srv:     srv,
		readyCh: make(chan struct{}),
		errCh:   make(chan error, 1),
		doneCh:  make(chan struct{}),
	}

	pw, err := runPlaywright()
	if err != nil {
		_ = srv.Close()
		return nil, err
	}
	p.pw = pw

	chromePath := ChromePath()
	// Widevine CDM is bundled inside Chrome at this well-known relative path.
	widevinePath := filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "WidevineCdm")

	// Playwright injects --mute-audio into every headless Chromium launch.
	// We must strip it so Chrome's audio service can route through
	// PulseAudio/PipeWire normally. Use headless when we have a saved token
	// (no auth UI needed); show a real window for first-run interactive login.
	headless := userToken != ""
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		ExecutablePath:    &chromePath,
		Headless:          &headless,
		IgnoreDefaultArgs: []string{"--mute-audio"},
		Args: []string{
			// Sandbox requires suid/namespace support unavailable from a non-system path.
			"--no-sandbox",
			"--disable-setuid-sandbox",
			"--autoplay-policy=no-user-gesture-required",
			"--enable-features=MediaCapabilities,WidevineCdm",
			"--disable-blink-features=AutomationControlled",
			"--widevine-path=" + widevinePath,
			// Suppress Chrome's built-in MPRIS D-Bus registration so our Go
			// MPRIS server (org.mpris.MediaPlayer2.vibez) is the sole player
			// visible to the desktop environment.
			"--disable-features=HardwareMediaKeyHandling,MediaSessionService",
		},
	})
	if err != nil {
		_ = pw.Stop()
		_ = srv.Close()
		return nil, fmt.Errorf("cdp: launch browser: %w", err)
	}
	p.browser = browser

	pg, err := browser.NewPage()
	if err != nil {
		_ = browser.Close()
		_ = pw.Stop()
		_ = srv.Close()
		return nil, fmt.Errorf("cdp: new page: %w", err)
	}
	p.page = pg

	// Forward page crashes and unhandled JS errors to errCh so WaitReady
	// returns immediately instead of waiting for the full timeout.
	pg.On("crash", func() {
		p.sendError(fmt.Errorf("cdp: Chrome page crashed"))
	})
	pg.On("pageerror", func(err error) {
		p.sendError(fmt.Errorf("cdp: page JS error: %w", err))
	})
	// Forward browser console messages to stderr for diagnostics.
	pg.On("console", func(msg playwright.ConsoleMessage) {
		if msg.Type() == "error" || msg.Type() == "warning" {
			fmt.Fprintf(os.Stderr, "[chrome %s] %s\n", msg.Type(), msg.Text())
		}
	})

	// Playwright's ExposeFunction wraps Go callbacks to return JS Promises
	// automatically — no Promise shim needed. goStreamURL is intentionally
	// absent; its absence triggers Chrome/Widevine mode in musickit.html.
	bindings := map[string]playwright.ExposedFunction{
		"goNeedsAuth": func(_ ...any) any { return nil },
		"goAuthComplete": func(_ ...any) any {
			select {
			case <-p.readyCh:
			default:
				close(p.readyCh)
			}
			return nil
		},
		"goPlayerStateChange": func(args ...any) any {
			if len(args) > 0 {
				if s, ok := args[0].(string); ok {
					var js jsState
					if json.Unmarshal([]byte(s), &js) == nil {
						p.applyState(js)
					}
				}
			}
			return nil
		},
		"goUserTokenChanged": func(args ...any) any {
			if len(args) > 0 {
				if tok, ok := args[0].(string); ok && tok != "" && p.OnUserToken != nil {
					p.OnUserToken(tok)
				}
			}
			return nil
		},
		"goStorefrontChanged": func(args ...any) any {
			if len(args) > 0 {
				if sf, ok := args[0].(string); ok && sf != "" && p.OnStorefront != nil {
					p.OnStorefront(sf)
				}
			}
			return nil
		},
		"goError": func(args ...any) any {
			msg := ""
			if len(args) > 0 {
				msg, _ = args[0].(string)
			}
			p.sendError(fmt.Errorf("musickit: %s", msg))
			return nil
		},
	}

	for name, fn := range bindings {
		if err := pg.ExposeFunction(name, fn); err != nil {
			_ = browser.Close()
			_ = pw.Stop()
			_ = srv.Close()
			return nil, fmt.Errorf("cdp: expose %s: %w", name, err)
		}
	}

	srvURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	go func() {
		if _, err := pg.Goto(srvURL); err != nil {
			select {
			case <-p.doneCh:
			default:
				p.sendError(fmt.Errorf("cdp navigate: %w", err))
			}
		}
	}()

	return p, nil
}

func (p *Player) sendError(err error) {
	select {
	case p.errCh <- err:
	default:
	}
	p.mu.Lock()
	p.state.Error = err.Error()
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

type jsState struct {
	IsPlaying   bool     `json:"isPlaying"`
	CurrentTime float64  `json:"currentTime"`
	Duration    float64  `json:"duration"`
	Volume      float64  `json:"volume"`
	RepeatMode  int      `json:"repeatMode"`
	ShuffleMode int      `json:"shuffleMode"`
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
		Playing:     js.IsPlaying,
		Position:    time.Duration(js.CurrentTime * float64(time.Second)),
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

func (p *Player) dispatch(js string) {
	go func() { _, _ = p.page.Evaluate(js) }()
}

// ── Lifecycle ─────────────────────────────────────────────────────────────

func (p *Player) Run() {
	<-p.doneCh
	_ = p.browser.Close()
	_ = p.pw.Stop()
	_ = p.srv.Close()
}

func (p *Player) Terminate() {
	select {
	case <-p.doneCh:
	default:
		close(p.doneCh)
	}
}

func (p *Player) WaitReady(ctx context.Context) error {
	select {
	case <-p.readyCh:
		return nil
	case err := <-p.errCh:
		return err
	case <-ctx.Done():
		return fmt.Errorf("cdp player: %w", ctx.Err())
	}
}

// ── player.Player interface ───────────────────────────────────────────────

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
	p.dispatch(fmt.Sprintf(`window.vibezSeek && window.vibezSeek(%f)`, position.Seconds()))
	return nil
}

func (p *Player) SetVolume(v float64) error {
	p.dispatch(fmt.Sprintf(`window.vibezSetVolume && window.vibezSetVolume(%f)`, v))
	return nil
}

func (p *Player) SetQueue(ids []string) error {
	expr, err := buildSetQueueJS(ids)
	if err != nil {
		return err
	}
	p.dispatch(expr)
	return nil
}

func (p *Player) SetPlaylist(playlistID string, startIdx int) error {
	expr, err := buildSetPlaylistJS(playlistID, startIdx)
	if err != nil {
		return err
	}
	p.dispatch(expr)
	return nil
}

func (p *Player) AppendQueue(ids []string) error {
	expr, err := buildAppendQueueJS(ids)
	if err != nil {
		return err
	}
	p.dispatch(expr)
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
	p.Terminate()
	return nil
}
