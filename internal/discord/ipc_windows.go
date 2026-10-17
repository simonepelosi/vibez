//go:build windows

package discord

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// candidateIPCPaths returns the named pipes the Discord desktop client may listen on.
func candidateIPCPaths() []string {
	candidates := make([]string, 0, ipcEndpointCount)
	for i := range ipcEndpointCount {
		candidates = append(candidates, fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i))
	}
	return candidates
}

// dialIPCPath connects to the Discord IPC named pipe at path. A missing pipe fails
// immediately; a pipe whose instances are all busy is retried until defaultTimeout.
func dialIPCPath(path string) (net.Conn, error) {
	timeout := defaultTimeout
	return winio.DialPipe(path, &timeout)
}
