//go:build linux || darwin || windows

package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// restartRoleEnv tells a copy of this test binary which side of a restart it
// plays in TestRestartProcess_HandsOverToTheNewBinary.
const restartRoleEnv = "VIBEZ_TEST_RESTART_ROLE"

// To whoever started vibez, the process an update restarts into has to be the
// same program carrying on: same arguments, environment and standard streams,
// and its exit status is the one they get back.
func TestRestartProcess_HandsOverToTheNewBinary(t *testing.T) {
	switch os.Getenv(restartRoleEnv) {
	case "old":
		restartAsOldBinary()
	case "new":
		runAsNewBinary()
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestRestartProcess_HandsOverToTheNewBinary$", "--", "two words", `quo"te`, `trailing\`}
	old := exec.Command(self, args...) //nolint:gosec // this test binary
	old.Env = append(os.Environ(), restartRoleEnv+"=old")
	old.Stdin = strings.NewReader("ping\n")
	var stdout, stderr bytes.Buffer
	old.Stdout, old.Stderr = &stdout, &stderr

	err = old.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("exit = %v, want status 7 from the restarted process\nstderr:\n%s", err, stderr.String())
	}
	if want := fmt.Sprintf("args=%q stdin=%q\n", args, "ping"); stdout.String() != want {
		t.Errorf("restarted process stdout = %q, want %q", stdout.String(), want)
	}
	if !strings.Contains(stderr.String(), "restarted stderr") {
		t.Errorf("restarted process stderr = %q, want it to reach the caller", stderr.String())
	}
}

// restartAsOldBinary plays the process that has just installed an update. It
// changes its own environment, as anything before the restart might, and
// restarts into this test binary, which then plays the new one.
func restartAsOldBinary() {
	self, err := os.Executable()
	if err == nil {
		err = os.Setenv(restartRoleEnv, "new")
	}
	if err == nil {
		err = restartProcess(self)
	}
	fmt.Fprintln(os.Stderr, "restartProcess returned:", err)
	os.Exit(2)
}

// runAsNewBinary reports what the restarted process was handed.
func runAsNewBinary() {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Printf("args=%q stdin=%q\n", os.Args[1:], strings.TrimSpace(line))
	fmt.Fprintln(os.Stderr, "restarted stderr")
	os.Exit(7)
}

// A restart that cannot start the new binary must come back to the caller, which
// reports it, rather than end the process quietly.
func TestRestartProcess_ReturnsWhenTheBinaryCannotStart(t *testing.T) {
	if err := restartProcess(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("restartProcess reported no error for a binary that does not exist")
	}
}
