//go:build linux || darwin || windows

package cdp

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	playwright "github.com/mxschmitt/playwright-go"
)

// The driver must never be handed a terminal file descriptor. os/exec passes an
// *os.File straight through to the child, so anything but a plain io.Writer
// lets the node driver reach the user's terminal — see driverio.go for what
// that costs.
func TestDriverRunOptions_NeverATerminal(t *testing.T) {
	opts, output := newDriverRunOptions(t.TempDir())
	if output == nil {
		t.Fatal("newDriverRunOptions returned a nil output buffer; driver diagnostics would have nowhere to go")
	}
	for name, w := range map[string]any{"Stdout": opts.Stdout, "Stderr": opts.Stderr} {
		if w == nil {
			t.Errorf("%s = nil; playwright-go defaults it to the process's own %s and hands the driver the user's terminal", name, strings.ToLower(name))
			continue
		}
		if f, ok := w.(*os.File); ok {
			t.Errorf("%s = *os.File(%q); os/exec passes the descriptor through unchanged, so the driver can reach the terminal", name, f.Name())
		}
	}
}

// Setting RunOptions.Stderr without RunOptions.Logger makes playwright-go
// redirect Go's process-wide standard logger to the same writer (run.go:452),
// which would silently discard log output for every package in the binary.
// Exercise the real dependency rather than trusting the read: this fails if
// the constructor stops setting Logger or playwright-go moves the branch.
func TestDriverRunOptions_LeaveTheStandardLoggerAlone(t *testing.T) {
	before := log.Writer()
	t.Cleanup(func() { log.SetOutput(before) })

	opts, _ := newDriverRunOptions(t.TempDir())
	if _, err := playwright.NewDriver(opts); err != nil {
		t.Fatalf("NewDriver: %v", err)
	}

	if after := log.Writer(); after != before {
		t.Errorf("playwright-go repointed the standard logger to %T; RunOptions.Logger must stay non-nil to avoid its log.SetOutput branch", after)
	}
}

func TestBoundedDriverOutput_KeepsTheTail(t *testing.T) {
	o := &boundedDriverOutput{data: make([]byte, 0, driverOutputLimit)}

	// Two writes that only overflow together, so the trim path runs rather
	// than the oversized-write shortcut.
	head := strings.Repeat("a", driverOutputLimit-10)
	if _, err := fmt.Fprint(o, head); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := fmt.Fprint(o, "0123456789tail"); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := o.String()
	if len(got) != driverOutputLimit {
		t.Errorf("len = %d, want %d: the buffer must stay bounded", len(got), driverOutputLimit)
	}
	if !strings.HasSuffix(got, "tail") {
		t.Error("the tail was dropped; the most recent output is the part worth keeping")
	}
}

func TestBoundedDriverOutput_SingleWriteOverTheLimit(t *testing.T) {
	o := &boundedDriverOutput{data: make([]byte, 0, driverOutputLimit)}
	if _, err := fmt.Fprint(o, strings.Repeat("x", driverOutputLimit*2)+"end"); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := o.String()
	if len(got) != driverOutputLimit {
		t.Errorf("len = %d, want %d", len(got), driverOutputLimit)
	}
	if !strings.HasSuffix(got, "end") {
		t.Error("a single oversized write should still leave its tail")
	}
}

// Write must report the full length it was given. io.Writer treats a short
// count as an error, so trimming internally while returning the trimmed count
// would make callers think the driver's own writes failed.
func TestBoundedDriverOutput_ReportsFullWriteLength(t *testing.T) {
	o := &boundedDriverOutput{data: make([]byte, 0, driverOutputLimit)}
	p := []byte(strings.Repeat("y", driverOutputLimit+512))
	n, err := o.Write(p)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(p) {
		t.Errorf("n = %d, want %d: a short count is an error to every io.Writer caller", n, len(p))
	}
}

func TestAddDriverOutput_LeavesSuccessAlone(t *testing.T) {
	o := &boundedDriverOutput{}
	_, _ = fmt.Fprint(o, "noise the driver produced while succeeding")
	if err := addDriverOutput(nil, o); err != nil {
		t.Errorf("addDriverOutput(nil, ...) = %v, want nil", err)
	}
}

func TestAddDriverOutput_SilentDriverKeepsTheErrorAsIs(t *testing.T) {
	want := errors.New("playwright driver: exec failed")
	got := addDriverOutput(want, &boundedDriverOutput{})
	if got != want {
		t.Errorf("got %v, want the original error unchanged: a driver that said nothing should not gain an empty section", got)
	}
}

func TestAddDriverOutput_AttachesOutputAndStaysUnwrappable(t *testing.T) {
	base := errors.New("exec: node: not found")
	o := &boundedDriverOutput{}
	_, _ = fmt.Fprint(o, "  node: symbol lookup error\n")

	got := addDriverOutput(fmt.Errorf("playwright driver: %w", base), o)
	if !errors.Is(got, base) {
		t.Error("the cause is no longer reachable with errors.Is; the wrap must use %w")
	}
	if !strings.Contains(got.Error(), "symbol lookup error") {
		t.Errorf("driver output missing from %q", got)
	}
	if strings.Contains(got.Error(), "output:\n  node") {
		t.Error("surrounding whitespace should be trimmed before the output is attached")
	}
}
