//go:build !windows

package discord

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// candidateIPCPaths returns a list of prospective Discord IPC socket paths.
func candidateIPCPaths() []string {
	var candidates []string
	dirs := socketDirs()
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for i := range ipcEndpointCount {
			candidates = append(candidates, filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i)))
		}
	}
	return candidates
}

func socketDirs() []string {
	dirs := []string{
		os.Getenv("XDG_RUNTIME_DIR"),
		filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "app", "com.discordapp.Discord"),
		filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "app", "com.discordapp.DiscordCanary"),
		filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "app", "com.discordapp.DiscordPTB"),
		filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "snap.discord"),
		os.Getenv("TMPDIR"),
		os.Getenv("TMP"),
		os.Getenv("TEMP"),
		os.TempDir(),
		"/tmp",
	}

	seen := make(map[string]bool)
	var unique []string
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			unique = append(unique, d)
		}
	}
	return unique
}

// dialIPCPath connects to the Discord IPC Unix domain socket at path.
func dialIPCPath(path string) (net.Conn, error) {
	return net.DialTimeout("unix", path, defaultTimeout)
}
