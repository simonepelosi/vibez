package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func buildBenchSegment(sampleCount, sampleSize uint32) []byte {
	sample := bytes.Repeat([]byte{0x5A}, int(sampleSize))
	totalSampleBytes := int(sampleCount) * int(sampleSize)

	var sencPayload bytes.Buffer
	sencPayload.Write([]byte{0x00, 0x00, 0x00, 0x02}) // version/flags: hasSubsamples
	sencPayload.Write(binary.BigEndian.AppendUint32(nil, sampleCount))
	iv := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	var subEntry [8]byte
	binary.BigEndian.PutUint16(subEntry[0:2], 1) // subsample count = 1
	binary.BigEndian.PutUint16(subEntry[2:4], 0) // clear bytes = 0
	binary.BigEndian.PutUint32(subEntry[4:8], sampleSize)
	for range sampleCount {
		sencPayload.Write(iv)
		sencPayload.Write(subEntry[:])
	}
	sencBox := makeBox("senc", sencPayload.Bytes())

	var trunPayload bytes.Buffer
	trunPayload.Write([]byte{0x00, 0x00, 0x02, 0x01}) // data_offset_present | sample_size_present
	trunPayload.Write(binary.BigEndian.AppendUint32(nil, sampleCount))
	trunOffsetPos := trunPayload.Len()
	trunPayload.Write([]byte{0x00, 0x00, 0x00, 0x00}) // placeholder data_offset
	var sizeBuf [4]byte
	binary.BigEndian.PutUint32(sizeBuf[:], sampleSize)
	for range sampleCount {
		trunPayload.Write(sizeBuf[:])
	}
	trunBox := makeBox("trun", trunPayload.Bytes())

	trafPayload := append(append([]byte(nil), trunBox...), sencBox...)
	moofBox := makeBox("moof", makeBox("traf", trafPayload))

	dataOffset := uint32(len(moofBox) + 8) //nolint:gosec // G115: synthetic benchmark moof fits uint32
	trunIdx := bytes.Index(moofBox, []byte("trun"))
	binary.BigEndian.PutUint32(moofBox[trunIdx+4+trunOffsetPos:trunIdx+8+trunOffsetPos], dataOffset)

	mdat := make([]byte, 8+totalSampleBytes)
	binary.BigEndian.PutUint32(mdat[0:4], uint32(len(mdat))) //nolint:gosec // G115: synthetic benchmark mdat fits uint32
	copy(mdat[4:8], "mdat")
	step := int(sampleSize)
	for off := 8; off < len(mdat); off += step {
		copy(mdat[off:off+step], sample)
	}

	return append(moofBox, mdat...)
}

func BenchmarkMakeADTSHeader(b *testing.B) {
	cfg := AudioConfig{ObjectType: 2, FreqIndex: 4, ChanConfig: 2}
	for b.Loop() {
		_ = MakeADTSHeader(384, cfg)
	}
}

func BenchmarkDecryptSegment(b *testing.B) {
	cfg := AudioConfig{ObjectType: 2, FreqIndex: 4, ChanConfig: 2}
	kid := make([]byte, 16)
	seg := buildBenchSegment(256, 384)
	nopDecrypt := func(_, _, input []byte, _ []Subsample) ([]byte, error) {
		return input, nil
	}

	b.ReportAllocs()
	for b.Loop() {
		out, err := DecryptSegment(seg, kid, cfg, nopDecrypt)
		if err != nil {
			b.Fatalf("DecryptSegment: %v", err)
		}
		if len(out) == 0 {
			b.Fatal("empty decrypted segment")
		}
	}
}
