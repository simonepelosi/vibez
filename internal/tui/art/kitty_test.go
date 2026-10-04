package art

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestKittyPreservesResolutionAndCellWidths(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	size := Size{32, 16}
	data, err := KittyUpload(src, 0x560001, size)
	if err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	for _, chunk := range strings.Split(data, "\x1b_G")[1:] {
		head, body, ok := strings.Cut(chunk, ";")
		if !ok {
			t.Fatal("missing graphics payload")
		}
		_ = head
		payload, _, _ := strings.Cut(body, "\x1b\\")
		encoded.WriteString(payload)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil || img.Bounds() != src.Bounds() {
		t.Fatalf("image resolution changed: %v", err)
	}
	for _, line := range KittyLines(0x560001, size) {
		if ansi.StringWidth(line) != size.Width {
			t.Fatalf("incorrect placeholder width: %d", ansi.StringWidth(line))
		}
	}
	if !strings.Contains(data, "U=1") || !strings.Contains(data, "q=2") {
		t.Fatal("must use silent virtual placement")
	}
}
