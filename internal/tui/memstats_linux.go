//go:build linux

package tui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readRSS returns the Resident Set Size in bytes for the given PID by
// parsing VmRSS from /proc/<pid>/status. Returns 0 on any error.
func readRSS(pid int) int64 {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

// findHelperRSS sums the RSS of all processes whose /proc/<pid>/exe
// resolves to one of the provided binary paths (i.e. all Chrome helper
// processes launched as vibez-helper).
func findHelperRSS(binPaths ...string) int64 {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	pathSet := make(map[string]bool, len(binPaths))
	for _, p := range binPaths {
		pathSet[p] = true
	}

	self := os.Getpid()
	var total int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			continue
		}
		if pathSet[exePath] {
			total += readRSS(pid)
		}
	}
	return total
}

// fmtBytes formats a byte count as a compact human-readable string.
func fmtBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%dM", b>>20)
	case b >= 1<<10:
		return fmt.Sprintf("%dK", b>>10)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// collectMemStats returns a human-readable RSS summary, e.g. "46M+285M"
// (vibez RSS + total helper RSS). When no helper process is found the
// helper component is omitted: "46M".
func collectMemStats(helperPaths []string) string {
	selfRSS := readRSS(os.Getpid())
	helperRSS := findHelperRSS(helperPaths...)
	self := fmtBytes(selfRSS)
	if helperRSS > 0 {
		return self + "+" + fmtBytes(helperRSS)
	}
	return self
}
