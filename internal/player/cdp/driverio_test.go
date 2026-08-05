package cdp

import (
	"os"
	"testing"
)

// The driver must never be handed a terminal file descriptor. os/exec passes
// an *os.File straight through to the child, so anything but a plain io.Writer
// lets the node driver reach the user's terminal — see driverio.go for what
// that costs.
func TestDriverOutput_NeverATerminal(t *testing.T) {
	w := driverOutput()
	if w == nil {
		t.Fatal("driverOutput() = nil; os/exec would hand the driver the parent's terminal")
	}
	if f, ok := w.(*os.File); ok {
		t.Fatalf("driverOutput() = *os.File(%q); os/exec passes the descriptor through unchanged, so the driver can reach the terminal", f.Name())
	}
}
