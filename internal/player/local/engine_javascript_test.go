package local

import (
	"os"
	"os/exec"
	"testing"
)

// TestEngineJS runs web/engine_test.mjs, the behaviour tests for the Chrome
// page engine used by the Windows backend, in Node. The engine is plain
// JavaScript with fakes standing in for the browser, so it is tested on every
// platform. Without Node the test skips locally but fails in CI, where a
// silently skipped suite would pass unnoticed.
func TestEngineJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required in CI to run web/engine_test.mjs")
		}
		t.Skip("node not found; skipping web/engine_test.mjs")
	}
	out, err := exec.Command(node, "web/engine_test.mjs").CombinedOutput() //nolint:gosec // node from PATH, fixed test script
	if err != nil {
		t.Fatalf("node web/engine_test.mjs: %v\n%s", err, out)
	}
}
