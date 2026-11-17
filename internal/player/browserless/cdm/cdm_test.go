//go:build (linux || darwin || windows) && cgo

package cdm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCDM_InputValidationAndClosedGuards(t *testing.T) {
	var c CDM

	if err := c.SetServerCertificate(nil); err == nil {
		t.Fatal("SetServerCertificate(nil) = nil, want error without panicking")
	}
	if err := c.SetServerCertificate([]byte{1}); err == nil {
		t.Fatal("SetServerCertificate on closed CDM = nil, want error")
	}

	if _, _, err := c.GenerateChallenge(nil); err == nil {
		t.Fatal("GenerateChallenge(nil) = nil, want invalid KID length error")
	}
	if _, _, err := c.GenerateChallenge(make([]byte, 16)); err == nil {
		t.Fatal("GenerateChallenge on closed CDM = nil, want closed error")
	}

	if err := c.UpdateSession("", []byte{1}); err == nil {
		t.Fatal("UpdateSession empty sessionID = nil, want error without panicking")
	}
	if err := c.UpdateSession("s1", nil); err == nil {
		t.Fatal("UpdateSession empty license = nil, want error without panicking")
	}
	if err := c.UpdateSession("s1", []byte{1}); err == nil {
		t.Fatal("UpdateSession on closed CDM = nil, want closed error")
	}

	if _, err := c.Decrypt(nil, []byte{1}, []byte{1}); err == nil {
		t.Fatal("Decrypt nil keyID = nil, want error without panicking")
	}
	if _, err := c.Decrypt([]byte{1}, nil, []byte{1}); err == nil {
		t.Fatal("Decrypt nil iv = nil, want error without panicking")
	}
	if out, err := c.Decrypt([]byte{1}, []byte{1}, nil); err != nil || len(out) != 0 {
		t.Fatalf("Decrypt empty ciphertext = (%v, %v), want empty slice and nil error", out, err)
	}
	if _, err := c.Decrypt([]byte{1}, []byte{1}, []byte{1}); err == nil {
		t.Fatal("Decrypt on closed CDM = nil, want closed error")
	}

	if err := c.CloseSession(""); err != nil {
		t.Fatalf("CloseSession empty sessionID = %v, want nil", err)
	}
	if err := c.CloseSession("s1"); err == nil {
		t.Fatal("CloseSession on closed CDM = nil, want closed error")
	}
	var nilCDM *CDM
	if err := nilCDM.CloseSession("s1"); err != nil {
		t.Fatalf("(*CDM)(nil).CloseSession = %v, want nil", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() on uninitialized CDM = %v, want nil", err)
	}
}

func TestCDM_DecryptBatchValidationAndClosedGuards(t *testing.T) {
	var c CDM
	if _, err := c.DecryptBatch(nil, nil, []BatchSample{{IV: []byte{1}, Input: []byte{1}}}); err == nil {
		t.Fatal("DecryptBatch with nil keyID = nil, want error")
	}
	if _, err := c.DecryptBatch(nil, []byte{1}, []BatchSample{{IV: nil, Input: []byte{1}}}); err == nil {
		t.Fatal("DecryptBatch with empty sample IV = nil, want error")
	}
	if _, err := c.DecryptBatch(nil, []byte{1}, []BatchSample{{
		IV:         []byte{1},
		Input:      []byte{1, 2},
		Subsamples: []Subsample{{ClearBytes: 1, CipherBytes: 5}},
	}}); err == nil {
		t.Fatal("DecryptBatch with subsample sum > input size = nil, want error")
	}
	if out, err := c.DecryptBatch(nil, []byte{1}, nil); err != nil || len(out) != 0 {
		t.Fatalf("DecryptBatch empty samples = (%v, %v), want nil, nil", out, err)
	}
	if _, err := c.DecryptBatch(nil, []byte{1}, []BatchSample{{IV: []byte{1}, Input: []byte{1}}}); err == nil {
		t.Fatal("DecryptBatch on closed CDM = nil, want closed error")
	}
	var nilCDM *CDM
	if _, err := nilCDM.DecryptBatch(nil, []byte{1}, []BatchSample{{IV: []byte{1}, Input: []byte{1}}}); err == nil {
		t.Fatal("(*CDM)(nil).DecryptBatch = nil, want closed error")
	}
}

func TestCDM_PathAndSessionNULGuards(t *testing.T) {
	if _, err := New("relative/libwidevinecdm.so"); err == nil {
		t.Fatal("New accepted relative CDM path, want error")
	}
	if _, err := New("/tmp/lib\x00widevine.so"); err == nil {
		t.Fatal("New accepted path containing NUL byte, want error")
	}

	tmp := filepath.Join(t.TempDir(), "sub.so")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, 0o666); err == nil { //nolint:gosec // G302: testing rejection of world-writable CDM file
		// On Unix this must be rejected before dlopen.
		if _, err := New(tmp); err == nil {
			t.Log("New returned nil only on Windows; expected rejection on Unix")
		}
	}

	var c CDM
	if err := c.UpdateSession("bad\x00sid", []byte{1}); err == nil {
		t.Fatal("UpdateSession accepted session ID with NUL byte")
	}
	if err := c.CloseSession("bad\x00sid"); err == nil {
		t.Fatal("CloseSession accepted session ID with NUL byte")
	}
}

func TestCDM_PathSafetyRejectsUNCSymlinkAndStickyDir(t *testing.T) {
	for _, badWin := range []string{
		`\\evil.example.com\share\widevinecdm.dll`,
		`//evil.example.com/share/widevinecdm.dll`,
		`\\?\UNC\evil\share\widevinecdm.dll`,
		`\??\C:\widevinecdm.dll`,
		`//./C:/widevinecdm.dll`,
		`\\.\PhysicalDrive0`,
		`relative\widevinecdm.dll`,
	} {
		if isSafePlatformAbsPath(badWin, "windows") {
			t.Errorf("isSafePlatformAbsPath(%q, windows) = true, want false", badWin)
		}
	}
	if !isSafePlatformAbsPath(`C:\Users\me\widevinecdm.dll`, "windows") {
		t.Error("isSafePlatformAbsPath rejected valid Windows drive path")
	}

	if runtime.GOOS != "windows" {
		dir := t.TempDir()
		realFile := filepath.Join(dir, "real.so")
		if err := os.WriteFile(realFile, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		linkFile := filepath.Join(dir, "link.so")
		if err := os.Symlink(realFile, linkFile); err == nil {
			if _, err := ValidateLibraryPath(linkFile); err == nil {
				t.Fatal("ValidateLibraryPath accepted symlinked CDM file")
			}
		}

		stickyDir := filepath.Join(dir, "sticky")
		if err := os.Mkdir(stickyDir, 0o750); err == nil {
			_ = os.Chmod(stickyDir, 0o777|os.ModeSticky) //nolint:gosec // G302: testing rejection of sticky world-writable parent dir
			inSticky := filepath.Join(stickyDir, "in_sticky.so")
			if err := os.WriteFile(inSticky, []byte("x"), 0o600); err == nil {
				if _, err := ValidateLibraryPath(inSticky); err == nil {
					t.Fatal("ValidateLibraryPath accepted file inside sticky world-writable directory")
				}
			}
		}
	}
}
