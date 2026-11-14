package main

import (
	"os"
	"runtime/debug"

	"github.com/simone-vibes/vibez/cmd"
)

// defaultMemoryLimitBytes sets a soft Go runtime memory ceiling so GC returns
// heap spikes promptly while still allowing an explicit GOMEMLIMIT override.
const defaultMemoryLimitBytes = 48 << 20

func main() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultMemoryLimitBytes)
	}
	cmd.Execute()
}
