//go:build linux || darwin || windows

package browserless

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/simone-vibes/vibez/internal/player/browserless/cdm"
)

func isSafeAbsCDMPath(p string) bool {
	return cdm.IsSafeAbsPath(p)
}

func hasValidCDMBinaryMagicForOS(hdr []byte, targetOS string) bool {
	if len(hdr) < 4 {
		return false
	}
	switch targetOS {
	case "windows":
		return hdr[0] == 'M' && hdr[1] == 'Z'
	case "linux":
		return hdr[0] == 0x7f && hdr[1] == 'E' && hdr[2] == 'L' && hdr[3] == 'F'
	case "darwin":
		magic := binary.BigEndian.Uint32(hdr[:4])
		switch magic {
		case 0xfeedface, 0xcefaedfe, 0xfeedfacf, 0xcffaedfe, 0xcafebabe, 0xbebafeca:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func verifyCDMBinaryMagic(path string) bool {
	f, err := cdm.OpenRegularNoFollow(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	return verifyCDMBinaryMagicFile(f)
}

func verifyCDMBinaryMagicFile(f *os.File) bool {
	var hdr [4]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	return hasValidCDMBinaryMagicForOS(hdr[:], runtime.GOOS)
}

func validateCDMCandidate(path string) (os.FileInfo, error) {
	fi, err := cdm.ValidateLibraryPath(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() <= 100000 {
		return nil, fmt.Errorf("candidate file too small (%d bytes)", fi.Size())
	}
	f, err := cdm.OpenRegularNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if !verifyCDMBinaryMagicFile(f) {
		return nil, errors.New("not a recognized CDM binary")
	}
	return fi, nil
}

func isValidCDMCandidate(path string) (os.FileInfo, bool) {
	fi, err := validateCDMCandidate(path)
	return fi, err == nil
}

func copyCDMFileAtomic(srcPath, destPath string) error {
	src, err := cdm.OpenRegularNoFollow(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	if !verifyCDMBinaryMagicFile(src) {
		return errors.New("source CDM file failed binary magic check")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(destPath), filepath.Base(destPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0700); err != nil {
		return err
	}
	n, err := io.Copy(tmp, io.LimitReader(src, maxCDMBytes+1))
	if err != nil {
		return err
	}
	if n > maxCDMBytes {
		return fmt.Errorf("cdm binary exceeds %d bytes limit", maxCDMBytes)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// FindCDM searches the host for an existing Widevine CDM library binary and
// returns the newest valid candidate so revoked older copies are skipped.
func FindCDM() string {
	p, _ := findCDM(nil)
	return p
}

func findCDM(report func(string)) (string, string) {
	var ignoredNotice string
	if envPath := os.Getenv("VIBEZ_CDM_PATH"); envPath != "" {
		if _, err := validateCDMCandidate(envPath); err == nil {
			return envPath, ""
		} else {
			ignoredNotice = fmt.Sprintf("ignoring VIBEZ_CDM_PATH (%v); falling back to CDM discovery", err)
			if report != nil {
				report(ignoredNotice)
			}
		}
	}

	var bestPath string
	var bestMod time.Time
	for _, p := range candidatePaths() {
		fi, ok := isValidCDMCandidate(p)
		if !ok {
			continue
		}
		if bestPath == "" || fi.ModTime().After(bestMod) {
			bestPath = p
			bestMod = fi.ModTime()
		}
	}
	return bestPath, ignoredNotice
}

// EnsureCDM returns an existing CDM library path, or automatically downloads
// and extracts the official standalone Widevine CDM component into the user cache directory.
func EnsureCDM(progress ...func(string)) (string, error) {
	var report func(string)
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	}

	p, ignoredNotice := findCDM(report)
	if p != "" {
		return p, nil
	}

	dest := DefaultCDMPath()
	if !isSafeAbsCDMPath(dest) {
		return "", errors.New("cannot determine safe user cache directory for Widevine CDM")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("failed to create cdm cache directory: %w", err)
	}

	libName := filepath.Base(dest)
	if home, err := os.UserHomeDir(); err == nil && isSafeAbsCDMPath(home) {
		cfgPath := filepath.Join(home, ".config", "vibez", libName)
		if lfi, lerr := os.Lstat(cfgPath); lerr == nil && lfi.Mode()&os.ModeSymlink == 0 {
			if _, ok := isValidCDMCandidate(cfgPath); ok {
				if err := copyCDMFileAtomic(cfgPath, dest); err == nil {
					return dest, nil
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	downloaded, err := DownloadCDM(ctx, dest, progress...)
	if err != nil {
		if ignoredNotice != "" {
			return "", fmt.Errorf("%s; %s not found and auto-download failed: %w (place %s manually in %s)", ignoredNotice, libName, err, libName, dest)
		}
		return "", fmt.Errorf("%s not found and auto-download failed: %w (place %s manually in %s)", libName, err, libName, dest)
	}
	return downloaded, nil
}
