package mp4

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// AudioConfig stores the audio metadata extracted from the init segment (esds box).
type AudioConfig struct {
	ObjectType int // 2 = AAC LC (profile = 1 in ADTS)
	FreqIndex  int // 4 = 44100 Hz, 3 = 48000 Hz, etc.
	ChanConfig int // 2 = Stereo, 1 = Mono
}

// DefaultAudioConfig returns the standard Apple Music AAC-LC 44.1kHz Stereo config.
func DefaultAudioConfig() AudioConfig {
	return AudioConfig{
		ObjectType: 2,
		FreqIndex:  4,
		ChanConfig: 2,
	}
}

// ParseAudioConfig parses the AudioSpecificConfig from the init segment (ftyp + moov).
func ParseAudioConfig(initData []byte) (AudioConfig, error) {
	cfg := DefaultAudioConfig()
	esdsIdx := bytes.Index(initData, []byte("esds"))
	if esdsIdx == -1 {
		return cfg, nil // Fallback to default
	}

	// Scan for tag 0x05 (DecSpecificInfoTag) within esds box
	boxEnd := min(len(initData), esdsIdx+128)
	esdsData := initData[esdsIdx:boxEnd]
	tag5Idx := bytes.Index(esdsData, []byte{0x05})
	if tag5Idx != -1 && tag5Idx+5 < len(esdsData) {
		// Variable length descriptor: length could be 0x80 0x80 0x80 0x02 or 0x02
		offset := tag5Idx + 1
		for offset < len(esdsData) && (esdsData[offset]&0x80) != 0 {
			offset++
		}
		offset++ // Skip length byte

		if offset+2 <= len(esdsData) {
			b1 := esdsData[offset]
			b2 := esdsData[offset+1]

			// AudioSpecificConfig bits:
			// 5 bits: audioObjectType
			// 4 bits: samplingFrequencyIndex
			// 4 bits: channelConfiguration
			cfg.ObjectType = int(b1 >> 3)
			cfg.FreqIndex = int(((b1 & 0x07) << 1) | (b2 >> 7))
			cfg.ChanConfig = int((b2 >> 3) & 0x0F)
		}
	}

	return cfg, nil
}

// MakeADTSHeader creates a standard 7-byte ADTS packet header for an AAC frame.
func MakeADTSHeader(aacFrameLen int, cfg AudioConfig) []byte {
	packetLen := aacFrameLen + 7
	header := make([]byte, 7)

	// Syncword: 12 bits 0xFFF
	header[0] = 0xFF
	header[1] = 0xF1 // MPEG-4, Layer 0, protection absent (no CRC)

	// Profile (2 bits), FreqIndex (4 bits), Private (1 bit), ChanConfig (3 bits)
	profile := (cfg.ObjectType - 1) & 0x03
	header[2] = byte(((profile << 6) | ((cfg.FreqIndex & 0x0F) << 2) | ((cfg.ChanConfig >> 2) & 0x01)) & 0xFF) //nolint:gosec // G115: bitwise masked to 8 bits
	header[3] = byte((((cfg.ChanConfig & 0x03) << 6) | ((packetLen >> 11) & 0x03)) & 0xFF)                     //nolint:gosec // G115: bitwise masked to 8 bits
	header[4] = byte((packetLen >> 3) & 0xFF)
	header[5] = byte((((packetLen & 0x07) << 5) | 0x1F) & 0xFF) //nolint:gosec // G115: bitwise masked to 8 bits
	header[6] = 0xFC                                            // Buffer fullness 0x7FF, number of AAC frames = 1 (index 0)

	return header
}

// Subsample represents a clear and encrypted byte range in a CENC sample.
type Subsample struct {
	ClearBytes  uint16
	CipherBytes uint32
}

// SampleEncInfo stores the IV and subsample encryption details for one sample.
type SampleEncInfo struct {
	IV         []byte
	Subsamples []Subsample
}

// DecryptFunc decrypts an audio sample buffer using AES-CTR (cenc).
type DecryptFunc func(kid, iv, input []byte, subsamples []Subsample) ([]byte, error)

// DecryptSegment parses a fragmented MP4 audio segment (moof + mdat) and uses
// the provided decrypt function to decrypt samples, returning an elementary AAC bitstream.
func DecryptSegment(
	segData []byte,
	kid []byte,
	cfg AudioConfig,
	decryptFn DecryptFunc,
) ([]byte, error) {
	// Find moof box
	moofIdx := bytes.Index(segData, []byte("moof"))
	if moofIdx < 4 {
		return nil, fmt.Errorf("moof box not found in segment")
	}
	moofStart := moofIdx - 4
	if moofStart+8 > len(segData) {
		return nil, fmt.Errorf("truncated moof box header")
	}
	moofSize := int(binary.BigEndian.Uint32(segData[moofStart : moofStart+4]))
	if moofSize < 8 || moofStart+moofSize > len(segData) {
		return nil, fmt.Errorf("invalid moof size: %d", moofSize)
	}
	moofData := segData[moofStart : moofStart+moofSize]

	// 1. Parse senc box
	sencIdx := bytes.Index(moofData, []byte("senc"))
	if sencIdx < 4 {
		return nil, fmt.Errorf("senc box not found in moof")
	}
	sencStart := sencIdx - 4
	if sencStart+16 > len(moofData) {
		return nil, fmt.Errorf("truncated senc box: need at least 16 bytes, got %d", len(moofData)-sencStart)
	}
	flags := binary.BigEndian.Uint32(moofData[sencStart+8:sencStart+12]) & 0x00FFFFFF
	hasSubsamples := (flags & 0x02) != 0
	sampleCount := binary.BigEndian.Uint32(moofData[sencStart+12 : sencStart+16])

	// Sanity-bound sampleCount against remaining senc buffer to prevent OOM
	remainingSencBytes := len(moofData) - (sencStart + 16)
	if remainingSencBytes < 0 || int64(sampleCount) > int64(remainingSencBytes)/8 || sampleCount > 65536 {
		return nil, fmt.Errorf("invalid or excessive sample count %d in senc (available bytes: %d)", sampleCount, remainingSencBytes)
	}

	samplesEnc := make([]SampleEncInfo, sampleCount)
	offset := sencStart + 16
	ivSize := 8 // Standard for Apple Music CENC

	for i := range sampleCount {
		if offset+ivSize > len(moofData) {
			return nil, fmt.Errorf("unexpected EOF in senc IVs at sample %d", i)
		}
		iv := make([]byte, ivSize)
		copy(iv, moofData[offset:offset+ivSize])
		offset += ivSize

		var subsamples []Subsample
		if hasSubsamples {
			if offset+2 > len(moofData) {
				return nil, fmt.Errorf("unexpected EOF in senc subsamples count at sample %d", i)
			}
			subCount := binary.BigEndian.Uint16(moofData[offset : offset+2])
			offset += 2
			remainingSubBytes := len(moofData) - offset
			if int(subCount) > remainingSubBytes/6 {
				return nil, fmt.Errorf("invalid subsample count %d at sample %d", subCount, i)
			}
			subsamples = make([]Subsample, subCount)
			for s := range subCount {
				if offset+6 > len(moofData) {
					return nil, fmt.Errorf("unexpected EOF in senc subsample entry")
				}
				clearBytes := binary.BigEndian.Uint16(moofData[offset : offset+2])
				cipherBytes := binary.BigEndian.Uint32(moofData[offset+2 : offset+6])
				offset += 6
				subsamples[s] = Subsample{
					ClearBytes:  clearBytes,
					CipherBytes: cipherBytes,
				}
			}
		}
		samplesEnc[i] = SampleEncInfo{
			IV:         iv,
			Subsamples: subsamples,
		}
	}

	// 2. Parse trun box
	trunIdx := bytes.Index(moofData, []byte("trun"))
	if trunIdx < 4 {
		return nil, fmt.Errorf("trun box not found in moof")
	}
	trunStart := trunIdx - 4
	if trunStart+16 > len(moofData) {
		return nil, fmt.Errorf("truncated trun box: need at least 16 bytes, got %d", len(moofData)-trunStart)
	}
	trunFlags := binary.BigEndian.Uint32(moofData[trunStart+8:trunStart+12]) & 0x00FFFFFF
	trunSampleCount := binary.BigEndian.Uint32(moofData[trunStart+12 : trunStart+16])
	if trunSampleCount != sampleCount {
		return nil, fmt.Errorf("sample count mismatch between senc (%d) and trun (%d)", sampleCount, trunSampleCount)
	}
	trunOffset := trunStart + 16

	dataOffset := int64(moofSize + 8) // Default fallback to right after moof + mdat header
	if (trunFlags & 0x01) != 0 {
		if trunOffset+4 > len(moofData) {
			return nil, fmt.Errorf("truncated trun data_offset")
		}
		// data_offset is a signed 32-bit integer per ISO/IEC 14496-12
		dataOffset = int64(int32(binary.BigEndian.Uint32(moofData[trunOffset : trunOffset+4]))) //nolint:gosec // G115: ISO/IEC 14496-12 defines data_offset as signed 32-bit
		trunOffset += 4
	}
	if (trunFlags & 0x04) != 0 {
		if trunOffset+4 > len(moofData) {
			return nil, fmt.Errorf("truncated trun first_sample_flags")
		}
		trunOffset += 4 // first_sample_flags
	}

	hasDuration := (trunFlags & 0x100) != 0
	hasSize := (trunFlags & 0x200) != 0
	hasFlags := (trunFlags & 0x400) != 0
	hasCompTime := (trunFlags & 0x800) != 0

	entrySize := 0
	if hasDuration {
		entrySize += 4
	}
	if hasSize {
		entrySize += 4
	}
	if hasFlags {
		entrySize += 4
	}
	if hasCompTime {
		entrySize += 4
	}

	if trunOffset+int(sampleCount)*entrySize > len(moofData) {
		return nil, fmt.Errorf("truncated trun sample entries")
	}

	sampleSizes := make([]int, sampleCount)
	for i := range sampleCount {
		if hasDuration {
			trunOffset += 4
		}
		if hasSize {
			sampleSizes[i] = int(binary.BigEndian.Uint32(moofData[trunOffset : trunOffset+4]))
			trunOffset += 4
		}
		if hasFlags {
			trunOffset += 4
		}
		if hasCompTime {
			trunOffset += 4
		}
	}

	// 3. Locate mdat and decrypt samples
	mdatTarget := int64(moofStart) + dataOffset
	if mdatTarget < 0 || mdatTarget > int64(len(segData)) {
		return nil, fmt.Errorf("invalid dataOffset %d: points to %d (seg length %d)", dataOffset, mdatTarget, len(segData))
	}
	mdatData := segData[mdatTarget:]
	var outBuf bytes.Buffer
	currMdatOffset := 0

	for i := 0; i < int(sampleCount); i++ {
		sSize := sampleSizes[i]
		if sSize < 0 || currMdatOffset+sSize > len(mdatData) {
			return nil, fmt.Errorf("sample %d exceeds mdat data bounds (size %d, remaining %d)", i, sSize, len(mdatData)-currMdatOffset)
		}
		sampleBytes := mdatData[currMdatOffset : currMdatOffset+sSize]
		currMdatOffset += sSize

		encInfo := samplesEnc[i]
		decryptedSample, err := decryptFn(kid, encInfo.IV, sampleBytes, encInfo.Subsamples)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt sample %d: %w", i, err)
		}

		// Prepend ADTS header
		adtsHeader := MakeADTSHeader(len(decryptedSample), cfg)
		outBuf.Write(adtsHeader)
		outBuf.Write(decryptedSample)
	}

	return outBuf.Bytes(), nil
}
