//go:build linux || darwin || windows

package cdp

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	playwright "github.com/mxschmitt/playwright-go"
)

// driverOutputLimit caps how much driver output is retained. The driver is
// chatty on failure and silent otherwise, so this only ever holds the tail of
// a session that is already going wrong.
const driverOutputLimit = 64 << 10

// playwrightLogger is the sink playwright-go logs its own runtime errors to.
//
// It is one shared instance rather than one per call because playwright-go
// keeps the logger in package state: `var logger = slog.Default()` (run.go:36)
// is reassigned from RunOptions.Logger on every transformRunOptions that
// supplies one (run.go:455). Its default writes to stderr, and it is used for
// things vibez cannot predict the timing of (navigation errors, WebSocket
// failures, BindingCall rejections), so leaving it at the default lets the
// library write over the TUI at any point in a session.
var playwrightLogger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

// boundedDriverOutput is what the Playwright driver process gets for stdout and
// stderr. It must never be the terminal, and it must not be io.Discard either.
//
// playwright.Run starts `node cli.js run-driver` through playwright-go's
// newPipeTransport, which pipes the child's stdin and stdout but passes
// RunOptions.Stderr through to cmd.Stderr untouched (transport.go:113). That
// defaults to os.Stderr, a live TTY file descriptor. Node initialises a TTY
// stream for it and snapshots the terminal's termios, by which point BubbleTea
// has already switched the terminal to raw mode, then re-applies that snapshot
// from its exit handler. The driver lives for the whole session and is stopped
// during shutdown, so node's restore lands *after* BubbleTea has put the
// terminal back into cooked mode and silently undoes it, leaving the shell with
// no echo, no line editing and no ctrl+c until the user closes the window.
//
// Being a plain io.Writer rather than an *os.File is what prevents that:
// os/exec gives the child a pipe for anything that is not an *os.File, so
// driver diagnostics can never scribble over the TUI. Discarding them was the
// first fix and it cost the only account of why the driver failed to start.
// Retaining a bounded tail keeps the terminal clean and the failure legible.
type boundedDriverOutput struct {
	mu   sync.Mutex
	data []byte
}

// newDriverRunOptions builds the options every playwright entry point in this
// package uses, so a call site that forgets one of them is a missing
// constructor call rather than a silently absent field.
func newDriverRunOptions(directory string) (*playwright.RunOptions, *boundedDriverOutput) {
	output := &boundedDriverOutput{data: make([]byte, 0, driverOutputLimit)}
	return &playwright.RunOptions{
		DriverDirectory:     directory,
		SkipInstallBrowsers: true,
		Stdout:              output,
		Stderr:              output,
		Logger:              playwrightLogger,
	}, output
}

// Write retains the last driverOutputLimit bytes written.
func (o *boundedDriverOutput) Write(p []byte) (int, error) {
	written := len(p)
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(p) >= driverOutputLimit {
		o.data = append(o.data[:0], p[len(p)-driverOutputLimit:]...)
		return written, nil
	}
	if overflow := len(o.data) + len(p) - driverOutputLimit; overflow > 0 {
		copy(o.data, o.data[overflow:])
		o.data = o.data[:len(o.data)-overflow]
	}
	o.data = append(o.data, p...)
	return written, nil
}

func (o *boundedDriverOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.data)
}

// addDriverOutput appends whatever the driver said to a failure from it. A
// driver that failed without saying anything returns err unchanged, so this
// never turns a one-line error into a multi-line one for nothing.
func addDriverOutput(err error, output *boundedDriverOutput) error {
	if err == nil {
		return nil
	}
	details := strings.TrimSpace(output.String())
	if details == "" {
		return err
	}
	return fmt.Errorf("%w\n\nPlaywright driver output:\n%s", err, details)
}

// os/exec creates a pipe for an io.Writer that is not an *os.File, which is
// what keeps node off the user's TTY. Keep that coverage explicit.
var _ io.Writer = (*boundedDriverOutput)(nil)
