package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func stubDecrypt(kid, iv, input []byte, subs []Subsample) ([]byte, error) {
	return input, nil
}

// buildSegment assembles a structurally valid moof+mdat segment.
// mdatFirst places mdat ahead of moof and uses a negative trun data_offset,
// which ISO/IEC 14496-12 permits and which the int32 handling must support.
func buildSegment(tb testing.TB, sampleData []byte, mdatFirst bool) []byte {
	tb.Helper()

	var senc bytes.Buffer
	senc.Write([]byte{0x00, 0x00, 0x00, 0x02}) // ver/flags, subsamples present
	senc.Write([]byte{0x00, 0x00, 0x00, 0x01}) // sample_count = 1
	senc.Write(bytes.Repeat([]byte{0xAA}, 8))  // IV
	senc.Write([]byte{0x00, 0x01})             // subsample count = 1
	senc.Write([]byte{0x00, 0x00})             // clear bytes
	protLen := make([]byte, 4)
	binary.BigEndian.PutUint32(protLen, uint32(len(sampleData))) //nolint:gosec // G115: test payload length fits uint32
	senc.Write(protLen)
	sencBox := makeBox("senc", senc.Bytes())

	var trun bytes.Buffer
	trun.Write([]byte{0x00, 0x00, 0x02, 0x01}) // data_offset + sample_size present
	trun.Write([]byte{0x00, 0x00, 0x00, 0x01}) // sample_count = 1
	offPos := trun.Len()
	trun.Write([]byte{0x00, 0x00, 0x00, 0x00}) // data_offset placeholder
	trun.Write(protLen)
	trunBox := makeBox("trun", trun.Bytes())

	trafPayload := append(append([]byte(nil), trunBox...), sencBox...)
	moofBox := makeBox("moof", makeBox("traf", trafPayload))
	mdatBox := makeBox("mdat", sampleData)

	trunIdx := bytes.Index(moofBox, []byte("trun"))
	if trunIdx == -1 {
		tb.Fatal("trun not found in synthetic moof")
	}
	patch := moofBox[trunIdx+4+offPos : trunIdx+8+offPos]

	if !mdatFirst {
		binary.BigEndian.PutUint32(patch, uint32(len(moofBox)+8)) //nolint:gosec // G115: synthetic segment size fits uint32
		return append(append([]byte(nil), moofBox...), mdatBox...)
	}

	// mdat first: payload sits at offset 8, moof starts at len(mdatBox).
	// data_offset is relative to moofStart, so it is negative here.
	dataOffset := int32(8 - len(mdatBox))                 //nolint:gosec // G115: synthetic segment size fits int32
	binary.BigEndian.PutUint32(patch, uint32(dataOffset)) //nolint:gosec // G115: deliberate signed-to-unsigned wire encoding
	return append(append([]byte(nil), mdatBox...), moofBox...)
}

// TestDecryptSegmentNegativeDataOffsetAccepted covers the case the signed
// int32 restoration exists for: mdat ahead of moof, so data_offset is negative
// and still points inside the segment. Read as unsigned this becomes ~4e9 and
// the bounds check rejects a valid segment.
func TestDecryptSegmentNegativeDataOffsetAccepted(t *testing.T) {
	sampleData := []byte("AACEncryptedPayload1234567890")
	seg := buildSegment(t, sampleData, true)

	got, err := DecryptSegment(seg, make([]byte, 16), DefaultAudioConfig(), stubDecrypt)
	if err != nil {
		t.Fatalf("valid segment with negative data_offset rejected: %v", err)
	}
	if want := 7 + len(sampleData); len(got) != want {
		t.Fatalf("expected %d bytes out, got %d", want, len(got))
	}
	if !bytes.Equal(got[7:], sampleData) {
		t.Errorf("payload mismatch: got %q", got[7:])
	}
}

func FuzzDecryptSegment(f *testing.F) {
	f.Add(buildSegment(f, []byte("AACEncryptedPayload1234567890"), false))
	f.Add(buildSegment(f, []byte("AACEncryptedPayload1234567890"), true))
	f.Add([]byte("moof"))
	f.Add([]byte{0x00, 0x00, 0x00, 0x10, 'm', 'o', 'o', 'f', 0x00, 0x00, 0x00, 0x08, 's', 'e', 'n', 'c'})
	f.Add([]byte{})

	kid := make([]byte, 16)
	cfg := DefaultAudioConfig()

	f.Fuzz(func(t *testing.T, data []byte) {
		// Only contract: never panic, never throw, on arbitrary network bytes.
		_, _ = DecryptSegment(data, kid, cfg, stubDecrypt)
	})
}
