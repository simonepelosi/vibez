//go:build linux

package browserless

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

// streamResult is what onFail saw and what the handler panicked with.
type streamResult struct {
	failID   string
	failErr  error
	panicked any
}

// serveStream runs handleStream for one prepared stream against an upstream
// that answers every segment with status.
func serveStream(t *testing.T, ctx context.Context, status int) (res streamResult) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(upstream.Close)

	s := &StreamServer{
		authToken: "tok",
		streams: map[string]*trackStream{
			"s1": {trackID: "123", baseURI: upstream.URL + "/", segments: []segmentRef{{uri: "seg0.mp4"}}},
		},
	}
	s.onFail = func(id string, err error) { res.failID, res.failErr = id, err }

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/stream/s1/tok.aac", nil)
	req.RemoteAddr = "127.0.0.1:40000"
	func() {
		defer func() { res.panicked = recover() }()
		s.handleStream(httptest.NewRecorder(), req)
	}()
	return res
}

func TestHandleStreamReportsUpstreamFailure(t *testing.T) {
	res := serveStream(t, context.Background(), http.StatusForbidden)
	id, err, panicked := res.failID, res.failErr, res.panicked

	if id != "s1" {
		t.Errorf("onFail stream ID = %q, want s1", id)
	}
	if err == nil || !strings.Contains(err.Error(), "segment 1/1: fetch: status 403") {
		t.Errorf("onFail error = %v, want the segment, stage and upstream status", err)
	}
	if e, ok := panicked.(error); !ok || !errors.Is(e, http.ErrAbortHandler) {
		t.Errorf("handler panicked with %v, want http.ErrAbortHandler so the client sees a truncated body", panicked)
	}
}

func TestHandleStreamCancelledIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := serveStream(t, ctx, http.StatusForbidden)
	err, panicked := res.failErr, res.panicked

	if err != nil {
		t.Errorf("onFail called with %v for a cancelled request, want no report", err)
	}
	if panicked != nil {
		t.Errorf("handler panicked with %v for a cancelled request, want a plain return", panicked)
	}
}

func TestStreamIDFromURL(t *testing.T) {
	got := streamIDFromURL("http://127.0.0.1:4321/stream/123-1700000000/abcdef.aac")
	if got != "123-1700000000" {
		t.Errorf("streamIDFromURL = %q, want 123-1700000000", got)
	}
}

func TestSeekTargetAndPlayerSeek(t *testing.T) {
	s := &StreamServer{
		streams: map[string]*trackStream{
			"s1": {
				trackID: "123",
				segments: []segmentRef{
					{uri: "seg0.mp4", duration: 6.0},
					{uri: "seg1.mp4", duration: 6.0},
					{uri: "seg2.mp4", duration: 6.0},
					{uri: "seg3.mp4", duration: 6.0},
				},
			},
		},
	}

	idx, offset, ok := s.SeekTarget("s1", 13*time.Second)
	if !ok || idx != 2 || offset != 12*time.Second {
		t.Fatalf("SeekTarget(13s) = (%d, %v, %v), want (2, 12s, true)", idx, offset, ok)
	}

	p := &Player{
		streamer:     s,
		curStream:    "s1",
		curStreamURL: "http://127.0.0.1:4321/stream/s1/tok.aac",
	}
	p.state.Track = &provider.Track{ID: "123", Duration: 24 * time.Second}

	if err := p.Seek(14 * time.Second); err != nil {
		t.Fatalf("Seek(14s) returned error: %v", err)
	}
	st, _ := p.GetState()
	if st.Position != 12*time.Second {
		t.Errorf("Position after Seek(14s) = %v, want 12s (start of segment 2)", st.Position)
	}
}

func TestExtractCDMFromCRX3(t *testing.T) {
	// Undersized payload should be rejected.
	smallCRX := buildFakeCRX3(t, "_platform_specific/linux_x64/libwidevinecdm.so", []byte("small"))
	if _, err := extractCDMFromCRX3(smallCRX); err == nil {
		t.Fatalf("expected undersized libwidevinecdm.so to be rejected")
	}

	// Valid >1 MiB payload should be extracted cleanly.
	soPayload := make([]byte, 1024*1024+64)
	copy(soPayload, []byte{0x7f, 'E', 'L', 'F', 'w', 'i', 'd', 'e', 'v', 'i', 'n', 'e'})
	crx := buildFakeCRX3(t, "_platform_specific/linux_x64/libwidevinecdm.so", soPayload)

	got, err := extractCDMFromCRX3(crx)
	if err != nil {
		t.Fatalf("extractCDMFromCRX3 failed: %v", err)
	}
	if !bytes.Equal(got, soPayload) {
		t.Errorf("extracted payload length = %d, want %d", len(got), len(soPayload))
	}

	if !isAllowedCDMHost("https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/test.crx3") {
		t.Errorf("expected edgedl.me.gvt1.com to be allowed")
	}
	if isAllowedCDMHost("http://edgedl.me.gvt1.com/test.crx3") {
		t.Errorf("expected non-https URL to be rejected")
	}
	if isAllowedCDMHost("https://evil.example.com/test.crx3") {
		t.Errorf("expected unknown host to be rejected")
	}
}

func buildFakeCRX3(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	fakeProtoHeader := []byte("crx3-proto-header")
	var crx bytes.Buffer
	crx.WriteString("Cr24")
	_ = binary.Write(&crx, binary.LittleEndian, uint32(3))
	_ = binary.Write(&crx, binary.LittleEndian, uint32(len(fakeProtoHeader)))
	crx.Write(fakeProtoHeader)
	crx.Write(zipBuf.Bytes())
	return crx.Bytes()
}


