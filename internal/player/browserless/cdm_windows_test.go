//go:build windows && cgo

package browserless

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player/browserless/cdm"
)

// Downloads the real Widevine CDM from Google's CDN and loads it in-process
// through the C++ bridge. This is the check that the MinGW-built host matches
// the MSVC-built CDM's ABI. It needs the network, so it is opt-in.
func TestWindowsRealCDMLoads(t *testing.T) {
	if os.Getenv("VIBEZ_TEST_NETWORK") != "1" {
		t.Skip("set VIBEZ_TEST_NETWORK=1 to download and load the real Widevine CDM")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir, err := os.MkdirTemp("", "cdm-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path, err := DownloadCDM(ctx, filepath.Join(dir, "widevinecdm.dll"))
	if err != nil {
		t.Fatalf("DownloadCDM: %v", err)
	}

	engine, err := cdm.New(path)
	if err != nil {
		t.Fatalf("cdm.New(%s): %v", path, err)
	}
	// Exercise CreateSessionAndGenerateRequest against a synthetic CENC PSSH box
	// to verify vtable slot alignment beyond Initialize/Destroy (including
	// Host_11::Allocate, OnResolveNewSessionPromise, and OnSessionMessage).
	pssh := []byte{
		0x00, 0x00, 0x00, 0x34, 'p', 's', 's', 'h',
		0x00, 0x00, 0x00, 0x00,
		0xed, 0xef, 0x8b, 0xa9, 0x79, 0xd6, 0x4a, 0xce,
		0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed,
		0x00, 0x00, 0x00, 0x14,
		0x08, 0x01, 0x12, 0x10,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
	}
	challenge, sessionID, err := engine.GenerateChallenge(pssh)
	if err != nil {
		t.Fatalf("GenerateChallenge: %v", err)
	}
	if len(challenge) == 0 || sessionID == "" {
		t.Fatalf("GenerateChallenge returned empty challenge (%d bytes) or sessionID (%q)", len(challenge), sessionID)
	}

	if err := engine.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
