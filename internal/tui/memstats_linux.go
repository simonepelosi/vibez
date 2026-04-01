//go:build linux

package tui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readPSS returns the Proportional Set Size in bytes for the given PID.
// PSS divides shared memory pages proportionally among all processes that
// map them, giving an accurate picture of unique physical memory cost.
// It reads /proc/<pid>/smaps_rollup (Linux 4.14+); falls back to VmRSS
// from /proc/<pid>/status on older kernels.
func readPSS(pid int) int64 {
	if pss := readSmapsRollup(pid); pss > 0 {
		return pss
	}
	return readVmRSS(pid)
}

func readSmapsRollup(pid int) int64 {
	f, err := os.Open(fmt.Sprintf("/proc/%d/smaps_rollup", pid))
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Pss:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

func readVmRSS(pid int) int64 {
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

// findHelperStats returns the total PSS and process count for all processes
// whose /proc/<pid>/exe resolves to one of the provided binary paths.
func findHelperStats(binPaths ...string) (totalPSS int64, count int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, 0
	}

	pathSet := make(map[string]bool, len(binPaths))
	for _, p := range binPaths {
		pathSet[p] = true
	}

	self := os.Getpid()
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
			totalPSS += readPSS(pid)
			count++
		}
	}
	return totalPSS, count
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

// collectMemStats returns a compact memory summary for the header, e.g.
// "55M+340M×6" where 340M is the total PSS of 6 vibez-helper processes.
// PSS (Proportional Set Size) counts shared library pages only once across
// all processes, giving a realistic view of actual physical memory cost.
func collectMemStats(helperPaths []string) string {
	selfPSS := readPSS(os.Getpid())
	helperPSS, count := findHelperStats(helperPaths...)
	self := fmtBytes(selfPSS)
	if helperPSS > 0 {
		return fmt.Sprintf("%s+%s×%d", self, fmtBytes(helperPSS), count)
	}
	return self
}
