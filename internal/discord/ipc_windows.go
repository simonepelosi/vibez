//go:build windows

package discord

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

var procGetNamedPipeServerProcessId = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNamedPipeServerProcessId")

// candidateIPCPaths returns the named pipes the Discord desktop client may listen on.
func candidateIPCPaths() []string {
	candidates := make([]string, 0, ipcEndpointCount)
	for i := range ipcEndpointCount {
		candidates = append(candidates, fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i))
	}
	return candidates
}

// dialIPCPath connects to the Discord IPC named pipe at path after verifying
// that both the pipe object owner SID and the serving process user SID match
// the current user.
func dialIPCPath(path string) (net.Conn, error) {
	timeout := defaultTimeout
	conn, err := winio.DialPipe(path, &timeout)
	if err != nil {
		return nil, err
	}
	if err := verifyPipePeerSID(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func verifyPipePeerSID(conn net.Conn) error {
	fc, ok := conn.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("discord ipc connection does not expose native pipe handle")
	}
	handle := windows.Handle(fc.Fd())
	if handle == 0 || handle == windows.InvalidHandle {
		return errors.New("discord ipc invalid native pipe handle")
	}

	curTok := windows.GetCurrentProcessToken()
	curUser, err := curTok.GetTokenUser()
	if err != nil || curUser == nil || curUser.User.Sid == nil {
		return errors.New("discord ipc failed to read current user SID")
	}

	sd, err := windows.GetSecurityInfo(handle, windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("discord ipc pipe owner query: %w", err)
	}
	owner, _, err := sd.Owner()
	// Elevated processes on Windows create pipes owned by their token's default
	// owner (typically BUILTIN\Administrators) rather than TokenUser; accept either
	// and rely on the server process TokenUser check below for exact user identity.
	if err != nil || owner == nil || (!owner.Equals(curUser.User.Sid) && !tokenOwnerMatches(curTok, owner)) {
		return errors.New("discord ipc pipe owner SID mismatch")
	}

	var serverPID uint32
	//nolint:gosec // G103: Win32 GetNamedPipeServerProcessId writes a uint32 PID into serverPID
	if r1, _, callErr := procGetNamedPipeServerProcessId.Call(uintptr(handle), uintptr(unsafe.Pointer(&serverPID))); r1 == 0 || serverPID == 0 {
		return fmt.Errorf("discord ipc pipe server PID query: %w", callErr)
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, serverPID)
	if err != nil {
		return fmt.Errorf("discord ipc pipe server open process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(proc) }()

	var procToken windows.Token
	if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &procToken); err != nil {
		return fmt.Errorf("discord ipc pipe server token query: %w", err)
	}
	defer func() { _ = procToken.Close() }()

	peerUser, err := procToken.GetTokenUser()
	if err != nil || peerUser == nil || peerUser.User.Sid == nil || !peerUser.User.Sid.Equals(curUser.User.Sid) {
		return errors.New("discord ipc pipe server user SID mismatch")
	}
	return nil
}

type winTokenOwner struct {
	Owner *windows.SID
}

func tokenOwnerMatches(tok windows.Token, owner *windows.SID) bool {
	var needed uint32
	err := windows.GetTokenInformation(tok, windows.TokenOwner, nil, 0, &needed)
	if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || needed < uint32(unsafe.Sizeof(winTokenOwner{})) {
		return false
	}
	buf := make([]byte, needed)
	if err := windows.GetTokenInformation(tok, windows.TokenOwner, &buf[0], needed, &needed); err != nil {
		return false
	}
	//nolint:gosec // G103: Win32 TOKEN_OWNER struct populated in buf by GetTokenInformation
	to := (*winTokenOwner)(unsafe.Pointer(&buf[0]))
	match := to.Owner != nil && owner.Equals(to.Owner)
	runtime.KeepAlive(buf)
	return match
}
