package cdp

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	playwright "github.com/mxschmitt/playwright-go"
)

func TestNewDriverRunOptions_DoNotRedirectStandardLogger(t *testing.T) {
	previous := log.Writer()
	var captured bytes.Buffer
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })

	options, _ := newDriverRunOptions("driver-dir")
	if _, err := playwright.NewDriver(options); err != nil {
		t.Fatalf("NewDriver: %v", err)
	}
	log.Print("standard logger remains configured")
	if !strings.Contains(captured.String(), "standard logger remains configured") {
		t.Fatal("playwright.NewDriver redirected the process-wide standard logger")
	}
}

func TestNewDriverRunOptions_KeepDriverOffTerminal(t *testing.T) {
	options, _ := newDriverRunOptions("driver-dir")
	if options.DriverDirectory != "driver-dir" {
		t.Fatalf("DriverDirectory = %q, want driver-dir", options.DriverDirectory)
	}
	if !options.SkipInstallBrowsers {
		t.Fatal("SkipInstallBrowsers = false, want true")
	}
	if options.Logger == nil {
		t.Fatal("Logger = nil; playwright-go would redirect the process-wide standard logger")
	}
	secondOptions, _ := newDriverRunOptions("other-driver-dir")
	if secondOptions.Logger != options.Logger {
		t.Fatal("newDriverRunOptions created a per-call package-global logger")
	}

	writers := map[string]io.Writer{"stdout": options.Stdout, "stderr": options.Stderr}
	for name, writer := range writers {
		if writer == nil {
			t.Fatalf("%s = nil; os/exec would hand the driver the parent's terminal", name)
		}
		if file, ok := writer.(*os.File); ok {
			t.Fatalf("%s = *os.File(%q); os/exec would pass the terminal descriptor through", name, file.Name())
		}
	}
}

func TestBoundedDriverOutput_KeepsRecentOutput(t *testing.T) {
	output := &boundedDriverOutput{data: make([]byte, 0, driverOutputLimit)}
	input := strings.Repeat("a", driverOutputLimit) + "fatal tail"
	written, err := output.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written != len(input) {
		t.Fatalf("Write returned %d, want %d", written, len(input))
	}
	got := output.String()
	if len(got) != driverOutputLimit {
		t.Fatalf("captured %d bytes, want %d", len(got), driverOutputLimit)
	}
	if !strings.HasSuffix(got, "fatal tail") {
		t.Fatalf("captured output does not retain recent tail: %q", got[len(got)-32:])
	}
}

func TestAddDriverOutput_PreservesCauseAndDetails(t *testing.T) {
	cause := errors.New("connection closed")
	output := &boundedDriverOutput{data: make([]byte, 0, driverOutputLimit)}
	_, _ = output.Write([]byte("FATAL: driver bundle corrupt"))

	got := addDriverOutput(cause, output)
	if !errors.Is(got, cause) {
		t.Fatalf("addDriverOutput() does not preserve cause: %v", got)
	}
	if !strings.Contains(got.Error(), "FATAL: driver bundle corrupt") {
		t.Fatalf("addDriverOutput() omitted driver details: %v", got)
	}
}
