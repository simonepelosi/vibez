//go:build windows

package local

import (
	_ "embed"
	"encoding/json"
	"fmt"

	playwright "github.com/mxschmitt/playwright-go"

	"github.com/simone-vibes/vibez/internal/player/cdp"
)

//go:embed web/player.html
var pageHTML []byte

//go:embed web/engine.js
var engineJS []byte

// snapshot is what the page engine reports about its audio element, both as
// the reply to a command and as an event. Gen names the track it describes
// (0 when none is loaded) and Seq orders snapshots across both paths.
type snapshot struct {
	Gen      uint64  `json:"gen"`
	Seq      uint64  `json:"seq"`
	Kind     string  `json:"kind"` // event type; empty in command replies
	Playing  bool    `json:"playing"`
	Loading  bool    `json:"loading"`
	Position float64 `json:"position"` // seconds
	Duration float64 `json:"duration"` // seconds; 0 until known
	Error    string  `json:"error"`    // why the command or the element failed
}

// engine runs commands on the page engine in web/engine.js. A command that
// fails for a media reason returns a snapshot with Error set; an error return
// means the page itself could not be reached.
type engine interface {
	call(method string, args ...any) (snapshot, error)
	close()
}

// pageEngine is the engine inside a headless Chrome page.
type pageEngine struct {
	page         playwright.Page
	closeBrowser func()
}

// openPage starts Chrome, loads the player page from media and routes the
// page's events to p. Page callbacks run on Playwright's goroutines, so none of
// them calls back into the page.
func openPage(media *mediaServer, p *Player) (*pageEngine, error) {
	page, closeBrowser, err := cdp.OpenBrowser(true, false)
	if err != nil {
		return nil, err
	}
	page.On("crash", func() { p.pageCrashed() })
	page.On("pageerror", func(err error) { p.sendLog("[chrome] page error: " + err.Error()) })
	page.On("console", func(msg playwright.ConsoleMessage) {
		if t := msg.Type(); t == "error" || t == "warning" {
			p.sendLog(fmt.Sprintf("[chrome %s] %s", t, msg.Text()))
		}
	})
	if err := page.ExposeFunction("goLocalEvent", func(args ...any) any {
		if len(args) > 0 {
			if raw, ok := args[0].(string); ok {
				p.onEvent(raw)
			}
		}
		return nil
	}); err != nil {
		closeBrowser()
		return nil, fmt.Errorf("expose page binding: %w", err)
	}
	if _, err := page.Goto(media.pageURL()); err != nil {
		closeBrowser()
		return nil, fmt.Errorf("open player page: %w", err)
	}
	ready, err := page.Evaluate(`() => typeof window.vibezLocal === 'object'`)
	if err != nil || ready != true {
		closeBrowser()
		return nil, fmt.Errorf("player page did not start (ready=%v): %v", ready, err)
	}
	return &pageEngine{page: page, closeBrowser: closeBrowser}, nil
}

func (e *pageEngine) call(method string, args ...any) (snapshot, error) {
	if args == nil {
		args = []any{}
	}
	v, err := e.page.Evaluate(`([method, args]) => window.vibezLocal.call(method, args)`, []any{method, args})
	if err != nil {
		return snapshot{}, fmt.Errorf("chrome %s: %w", method, err)
	}
	raw, ok := v.(string)
	if !ok {
		return snapshot{}, fmt.Errorf("chrome %s: reply is %T, not JSON text", method, v)
	}
	var s snapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return snapshot{}, fmt.Errorf("chrome %s: %w", method, err)
	}
	return s, nil
}

// close shuts Chrome and the Playwright driver down; a call still waiting on
// the page fails instead of hanging.
func (e *pageEngine) close() { e.closeBrowser() }
