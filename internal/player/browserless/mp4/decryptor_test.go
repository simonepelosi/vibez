package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMakeADTSHeader(t *testing.T) {
	cfg := AudioConfig{
		ObjectType: 2, // AAC-LC (profile 1)
		FreqIndex:  4, // 44100 Hz
		ChanConfig: 2, // Stereo
	}
	hdr := MakeADTSHeader(104, cfg)
	if len(hdr) != 7 {
		t.Fatalf("expected 7-byte header, got %d", len(hdr))
	}
	// Check syncword
	if hdr[0] != 0xFF || (hdr[1]&0xF0) != 0xF0 {
		t.Errorf("invalid ADTS syncword: %x %x", hdr[0], hdr[1])
	}
	// Verify length encoding: frame length = 104 + 7 = 111 (0x006F)
	frameLen := (int(hdr[3]&0x03) << 11) | (int(hdr[4]) << 3) | (int(hdr[5]>>5) & 0x07)
	if frameLen != 111 {
		t.Errorf("expected frame length 111, got %d", frameLen)
	}
}

func TestParseAudioConfigDefault(t *testing.T) {
	cfg, err := ParseAudioConfig([]byte("dummy data without esds"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ObjectType != 2 || cfg.FreqIndex != 4 || cfg.ChanConfig != 2 {
		t.Errorf("expected default audio config, got %+v", cfg)
	}
}

func TestParseAudioConfigValid(t *testing.T) {
	// Construct a synthetic esds box with tag 0x05 and payload 0x12 0x10 (AAC-LC, 44100, 2)
	var esds bytes.Buffer
	esds.WriteString("esds")
	esds.Write([]byte{0x00, 0x00, 0x00, 0x00}) // ver/flags
	esds.Write([]byte{0x05, 0x02, 0x12, 0x10}) // tag 0x05, len 2, config 0x12 0x10

	cfg, err := ParseAudioConfig(esds.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ObjectType != 2 || cfg.FreqIndex != 4 || cfg.ChanConfig != 2 {
		t.Errorf("expected parsed audio config 2/4/2, got %+v", cfg)
	}
}

func TestDecryptSegmentTruncated(t *testing.T) {
	dummyDecrypt := func(kid, iv, input []byte, subs []Subsample) ([]byte, error) {
		return input, nil
	}
	cfg := AudioConfig{ObjectType: 2, FreqIndex: 4, ChanConfig: 2}

	// Empty input
	_, err := DecryptSegment(nil, make([]byte, 16), cfg, dummyDecrypt)
	if err == nil {
		t.Error("expected error for empty segment, got nil")
	}

	// Corrupt / truncated box header (< 8 bytes)
	_, err = DecryptSegment([]byte{0x00, 0x00, 0x00}, make([]byte, 16), cfg, dummyDecrypt)
	if err == nil {
		t.Error("expected error for short box header, got nil")
	}

	// Box claim larger than buffer
	_, err = DecryptSegment([]byte{0x00, 0x01, 0x00, 0x00, 'm', 'o', 'o', 'f'}, make([]byte, 16), cfg, dummyDecrypt)
	if err == nil {
		t.Error("expected error for box size exceeding buffer, got nil")
	}
}

func makeBox(tag string, payload []byte) []byte {
	buf := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(buf))) //nolint:gosec // G115: test payload length fits uint32
	copy(buf[4:8], tag)
	copy(buf[8:], payload)
	return buf
}

func TestDecryptSegmentCorruptSampleCountOOM(t *testing.T) {
	dummyDecrypt := func(kid, iv, input []byte, subs []Subsample) ([]byte, error) {
		return input, nil
	}
	cfg := AudioConfig{ObjectType: 2, FreqIndex: 4, ChanConfig: 2}

	// Construct moof with a trun box containing a maliciously huge sample_count (e.g. 0x7FFFFFFF)
	var trunPayload bytes.Buffer
	trunPayload.Write([]byte{0x00, 0x00, 0x02, 0x01})
	trunPayload.Write([]byte{0x7F, 0xFF, 0xFF, 0xFF}) // 2 billion
	trunPayload.Write([]byte{0x00, 0x00, 0x00, 0x00}) // data_offset

	trunBox := makeBox("trun", trunPayload.Bytes())
	trafBox := makeBox("traf", trunBox)
	moofBox := makeBox("moof", trafBox)
	mdatBox := makeBox("mdat", nil)

	seg := append([]byte(nil), moofBox...)
	seg = append(seg, mdatBox...)
	_, err := DecryptSegment(seg, make([]byte, 16), cfg, dummyDecrypt)
	if err == nil {
		t.Error("expected error for excessive sample_count, got nil")
	}
}

func TestDecryptSegmentSyntheticEndToEnd(t *testing.T) {
	cfg := AudioConfig{ObjectType: 2, FreqIndex: 4, ChanConfig: 2}
	sampleData := []byte("AACEncryptedPayload1234567890")
	kid := make([]byte, 16)

	// Build senc box: 1 sample with 8-byte IV and subsample table
	var sencPayload bytes.Buffer
	sencPayload.Write([]byte{0x00, 0x00, 0x00, 0x02}) // ver/flags
	sencPayload.Write([]byte{0x00, 0x00, 0x00, 0x01}) // sample_count = 1
	iv := bytes.Repeat([]byte{0xAA}, 8)
	sencPayload.Write(iv)
	sencPayload.Write([]byte{0x00, 0x01}) // subsample count = 1
	sencPayload.Write([]byte{0x00, 0x00}) // clear bytes = 0
	protLen := uint32(len(sampleData))    //nolint:gosec // G115: sampleData length fits in uint32
	protLenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(protLenBuf, protLen)
	sencPayload.Write(protLenBuf)
	sencBox := makeBox("senc", sencPayload.Bytes())

	// Build trun box: 1 sample, data_offset_present (0x01) | sample_size_present (0x200)
	var trunPayload bytes.Buffer
	trunPayload.Write([]byte{0x00, 0x00, 0x02, 0x01})
	trunPayload.Write([]byte{0x00, 0x00, 0x00, 0x01})
	trunOffsetPos := trunPayload.Len()
	trunPayload.Write([]byte{0x00, 0x00, 0x00, 0x00}) // placeholder for data_offset
	trunPayload.Write(protLenBuf)
	trunBox := makeBox("trun", trunPayload.Bytes())

	trafPayload := append([]byte(nil), trunBox...)
	trafPayload = append(trafPayload, sencBox...)
	trafBox := makeBox("traf", trafPayload)
	moofBox := makeBox("moof", trafBox)

	// Fix data_offset in trunBox inside moofBox:
	// dataOffset points to sampleData inside mdat (which is right after moof + 8 byte mdat header)
	dataOffset := uint32(len(moofBox) + 8) //nolint:gosec // G115: test size fits uint32
	trunIdx := bytes.Index(moofBox, []byte("trun"))
	if trunIdx == -1 {
		t.Fatal("trun box not found")
	}
	binary.BigEndian.PutUint32(moofBox[trunIdx+4+trunOffsetPos:trunIdx+8+trunOffsetPos], dataOffset)

	mdatBox := makeBox("mdat", sampleData)
	seg := append([]byte(nil), moofBox...)
	seg = append(seg, mdatBox...)

	decryptInvoked := false
	mockDecrypt := func(k, ivBytes, in []byte, subs []Subsample) ([]byte, error) {
		decryptInvoked = true
		if !bytes.Equal(ivBytes, iv) {
			t.Errorf("IV mismatch: expected %x, got %x", iv, ivBytes)
		}
		if len(subs) != 1 || subs[0].CipherBytes != protLen {
			t.Errorf("subsample mismatch: got %+v", subs)
		}
		return bytes.Repeat([]byte{0xEE}, len(in)), nil
	}

	result, err := DecryptSegment(seg, kid, cfg, mockDecrypt)
	if err != nil {
		t.Fatalf("DecryptSegment failed: %v", err)
	}
	if !decryptInvoked {
		t.Fatal("decrypt function was never invoked")
	}
	expectedLen := 7 + len(sampleData)
	if len(result) != expectedLen {
		t.Fatalf("expected output length %d, got %d", expectedLen, len(result))
	}
	if result[0] != 0xFF || (result[1]&0xF0) != 0xF0 {
		t.Errorf("invalid ADTS header in result: %x %x", result[0], result[1])
	}
}

func TestDecryptSegmentSignedDataOffset(t *testing.T) {
	dummyDecrypt := func(kid, iv, input []byte, subs []Subsample) ([]byte, error) {
		return input, nil
	}
	cfg := AudioConfig{ObjectType: 2, FreqIndex: 4, ChanConfig: 2}

	var sencPayload bytes.Buffer
	sencPayload.Write([]byte{0x00, 0x00, 0x00, 0x00})
	sencPayload.Write([]byte{0x00, 0x00, 0x00, 0x01})
	sencPayload.Write(bytes.Repeat([]byte{0x01}, 8))
	sencBox := makeBox("senc", sencPayload.Bytes())

	var trunPayload bytes.Buffer
	trunPayload.Write([]byte{0x00, 0x00, 0x02, 0x01})
	trunPayload.Write([]byte{0x00, 0x00, 0x00, 0x01})
	// Negative data_offset: -10 = 0xFFFFFFF6
	trunPayload.Write([]byte{0xFF, 0xFF, 0xFF, 0xF6})
	trunPayload.Write([]byte{0x00, 0x00, 0x00, 0x0A})
	trunBox := makeBox("trun", trunPayload.Bytes())

	trafPayload := append([]byte(nil), trunBox...)
	trafPayload = append(trafPayload, sencBox...)
	trafBox := makeBox("traf", trafPayload)
	moofBox := makeBox("moof", trafBox)
	mdatBox := makeBox("mdat", bytes.Repeat([]byte{0x22}, 10))

	seg := append([]byte(nil), moofBox...)
	seg = append(seg, mdatBox...)
	_, err := DecryptSegment(seg, make([]byte, 16), cfg, dummyDecrypt)
	if err == nil {
		t.Error("expected error for negative out-of-bounds offset, got nil")
	}
}
