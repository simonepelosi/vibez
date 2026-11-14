//go:build !windows

package discord

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// candidateIPCPaths returns a list of prospective Discord IPC socket paths.
func candidateIPCPaths() []string {
	var candidates []string
	dirs := socketDirs()
	for _, dir := range dirs {
		for i := range ipcEndpointCount {
			candidates = append(candidates, filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i)))
		}
	}
	return candidates
}

func socketDirs() []string {
	var dirs []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" && filepath.IsAbs(xdg) {
		dirs = append(dirs,
			xdg,
			filepath.Join(xdg, "app", "com.discordapp.Discord"),
			filepath.Join(xdg, "app", "com.discordapp.DiscordCanary"),
			filepath.Join(xdg, "app", "com.discordapp.DiscordPTB"),
			filepath.Join(xdg, "snap.discord"),
		)
	}
	dirs = append(dirs,
		os.Getenv("TMPDIR"),
		os.Getenv("TMP"),
		os.Getenv("TEMP"),
		os.TempDir(),
		"/tmp",
	)

	seen := make(map[string]bool)
	var unique []string
	for _, d := range dirs {
		if d != "" && filepath.IsAbs(d) && !seen[d] {
			seen[d] = true
			unique = append(unique, d)
		}
	}
	return unique
}

// dialIPCPath connects to the Discord IPC Unix domain socket at path after
// verifying that it is a Unix socket owned by the current user and that the
// listening peer process also belongs to the current user.
func dialIPCPath(path string) (net.Conn, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, errors.New("discord ipc path is not a socket")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return nil, errors.New("discord ipc socket is owned by another user")
	}
	conn, err := (&net.Dialer{Timeout: defaultTimeout}).DialContext(context.Background(), "unix", path)
	if err != nil {
		return nil, err
	}
	if err := verifyPeerUID(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
