//go:build windows

package cdp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func baseDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Local", "vibez")
}

func chromeCandidates() []string {
	var paths []string
	for _, name := range []string{"LOCALAPPDATA", "ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if dir := os.Getenv(name); dir != "" {
			paths = append(paths, filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	// App Paths also covers Chrome installed outside the standard folders.
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			key, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`, registry.QUERY_VALUE|view)
			if err != nil {
				continue
			}
			path, kind, err := key.GetStringValue("")
			_ = key.Close()
			if err != nil {
				continue
			}
			if kind == registry.EXPAND_SZ {
				path, err = registry.ExpandString(path)
				if err != nil {
					continue
				}
			}
			if path = strings.Trim(path, `"`); path != "" {
				paths = append(paths, path)
			}
		}
	}
	if path, err := exec.LookPath("chrome.exe"); err == nil {
		paths = append(paths, path)
	}
	return paths
}
