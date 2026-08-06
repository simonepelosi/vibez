package cdp

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	playwright "github.com/mxschmitt/playwright-go"
)

const driverOutputLimit = 64 << 10

// playwright-go stores RunOptions.Logger in package-global state. Reusing one
// stable sink avoids redirecting the standard logger or an existing driver's
// Go-side logs when another driver starts. Node diagnostics are captured
// separately through Stdout and Stderr below.
var playwrightLogger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

var (
	errPlaywrightActive = errors.New("a Playwright driver is already active")
	playwrightActive    atomic.Bool
)

type managedPlaywright struct {
	*playwright.Playwright
	stopOnce sync.Once
	stopErr  error
}

func claimPlaywright() error {
	if !playwrightActive.CompareAndSwap(false, true) {
		return errPlaywrightActive
	}
	return nil
}

func releasePlaywright() {
	if !playwrightActive.CompareAndSwap(true, false) {
		panic("cdp: released Playwright without an active driver")
	}
}

func (p *managedPlaywright) Stop() error {
	p.stopOnce.Do(func() {
		defer releasePlaywright()
		p.stopErr = p.Playwright.Stop()
	})
	return p.stopErr
}

// boundedDriverOutput keeps the Playwright child on a pipe while retaining
// enough recent output to make startup failures actionable.
type boundedDriverOutput struct {
	mu   sync.Mutex
	data []byte
}

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

func installPlaywright(directory string) error {
	if err := claimPlaywright(); err != nil {
		return err
	}
	defer releasePlaywright()

	driverOptions, driverOutput := newDriverRunOptions(directory)
	return addDriverOutput(playwright.Install(driverOptions), driverOutput)
}

func runPlaywright() (*managedPlaywright, *boundedDriverOutput, error) {
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	if err := claimPlaywright(); err != nil {
		return nil, nil, err
	}

	driverOptions, driverOutput := newDriverRunOptions(driverDir())
	pw, err := playwright.Run(driverOptions)
	if err != nil {
		releasePlaywright()
		return nil, nil, fmt.Errorf("playwright driver: %w", addDriverOutput(err, driverOutput))
	}
	return &managedPlaywright{Playwright: pw}, driverOutput, nil
}

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

func addDriverOutput(err error, output *boundedDriverOutput) error {
	if err == nil {
		return nil
	}
	if output == nil {
		return err
	}
	details := strings.TrimSpace(output.String())
	if details == "" {
		return err
	}
	return fmt.Errorf("%w\n\nPlaywright driver output:\n%s", err, details)
}

// Keep compile-time interface coverage explicit: os/exec creates a pipe for an
// io.Writer that is not an *os.File, preventing Node from inheriting the TTY.
var _ io.Writer = (*boundedDriverOutput)(nil)
