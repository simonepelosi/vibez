package cdp

import "io"

// driverOutput is what the Playwright driver process gets for stdout/stderr.
// It must never be the terminal.
//
// playwright-go defaults both to os.Stdout/os.Stderr, which hands the node
// driver a live TTY file descriptor. Node snapshots that terminal's termios
// at startup — by which point BubbleTea has already switched the terminal to
// raw mode — and re-applies the snapshot from its exit handler. Shutdown waits
// for the driver to exit (Player.Run stops the browser and the driver), so
// node's restore lands *after* BubbleTea has put the terminal back into cooked
// mode and silently undoes it, leaving the shell with no echo, no line editing
// and no ctrl+c until the user closes the window.
//
// Returning a plain io.Writer rather than an *os.File also makes os/exec give
// the child a pipe, so driver diagnostics can never scribble over the TUI.
func driverOutput() io.Writer { return io.Discard }
