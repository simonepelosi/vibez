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

	path, err := DownloadCDM(ctx, filepath.Join(t.TempDir(), "widevinecdm.dll"))
	if err != nil {
		t.Fatalf("DownloadCDM: %v", err)
	}

	engine, err := cdm.New(path)
	if err != nil {
		t.Fatalf("cdm.New(%s): %v", path, err)
	}
	if err := engine.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
