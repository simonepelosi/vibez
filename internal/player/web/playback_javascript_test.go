package web

import (
	"os"
	"os/exec"
	"testing"
)

// TestPlaybackJS runs playback_test.mjs, which loads the queue and playback
// functions straight out of musickit.html into Node with a fake MusicKit.
// Without Node the test skips locally but fails in CI, where a silently
// skipped suite would pass unnoticed.
func TestPlaybackJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required in CI to run playback_test.mjs")
		}
		t.Skip("node not found; skipping playback_test.mjs")
	}
	out, err := exec.Command(node, "--test", "playback_test.mjs").CombinedOutput() //nolint:gosec // node from PATH, fixed test script
	if err != nil {
		t.Fatalf("node --test playback_test.mjs: %v\n%s", err, out)
	}
}
