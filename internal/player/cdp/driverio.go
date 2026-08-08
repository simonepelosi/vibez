package cdp

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	playwright "github.com/mxschmitt/playwright-go"
)

const driverOutputLimit = 64 << 10

// playwright-go stores RunOptions.Logger in package-global state. Reusing one
// stable sink prevents it from redirecting the process-wide standard logger.
var playwrightLogger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))

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
	details := strings.TrimSpace(output.String())
	if details == "" {
		return err
	}
	return fmt.Errorf("%w\n\nPlaywright driver output:\n%s", err, details)
}

// Keep compile-time interface coverage explicit: os/exec creates a pipe for an
// io.Writer that is not an *os.File, preventing Node from inheriting the TTY.
var _ io.Writer = (*boundedDriverOutput)(nil)
