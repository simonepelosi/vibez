package cmd

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// restartProcess hands this process over to exe, the binary an update just
// installed. Windows has no exec, so exe starts as a child with this process's
// arguments, environment, working directory and standard handles, and this
// process only waits for it and then exits with its status. Exiting straight
// away instead would tell the shell that started vibez it had finished, and
// the shell would take the console back while the new TUI still needs it. It
// returns only when the child cannot be started or waited for.
func restartProcess(exe string) error {
	child := &exec.Cmd{
		Path:   exe,
		Args:   os.Args,
		Env:    os.Environ(),
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	if err := child.Start(); err != nil {
		return err
	}
	// Ctrl+C and Ctrl+Break go to every process on the console. The child
	// decides what they mean; this one has to outlive it to report its status.
	// Notify installs a handler in this process only, after the child started,
	// so unlike an ignore flag nothing about it is inherited and the child
	// still gets both.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	err := child.Wait()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &exitErr):
		os.Exit(exitErr.ExitCode())
	}
	return err
}
