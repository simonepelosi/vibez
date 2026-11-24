package mp4

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
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

// readDescriptorHeader reads an ISO/IEC 14496-1 expandable descriptor tag and
// length from data[offset:], returning the tag, payload length, and the offset
// of the payload bytes.
func readDescriptorHeader(data []byte, offset int) (tag byte, length, payloadOffset int, ok bool) {
	if offset >= len(data) {
		return 0, 0, 0, false
	}
	tag = data[offset]
	offset++
	for range 4 {
		if offset >= len(data) {
			return 0, 0, 0, false
		}
		b := data[offset]
		offset++
		length = (length << 7) | int(b&0x7F)
		if (b & 0x80) == 0 {
			return tag, length, offset, true
		}
	}
	return 0, 0, 0, false
}

func applyAudioSpecificConfig(asc []byte, cfg *AudioConfig) bool {
	if len(asc) < 2 {
		return false
	}
	b1, b2 := asc[0], asc[1]
	objType := int(b1 >> 3)
	freqIdx := int(((b1 & 0x07) << 1) | (b2 >> 7))
	chanCfg := int((b2 >> 3) & 0x0F)
	// Sanity-check extracted fields: AAC-LC/SBR/PS (1..5), valid sample rate index (0..12), 1..7 channels.
	if objType < 1 || objType > 5 || freqIdx > 12 || chanCfg < 1 || chanCfg > 7 {
		return false
	}
	// ADTS profile is 2 bits (audioObjectType - 1); HE-AAC (SBR=5) signals base AAC-LC (2) in ADTS headers.
	if objType > 4 {
		objType = 2
	}
	cfg.ObjectType = objType
	cfg.FreqIndex = freqIdx
	cfg.ChanConfig = chanCfg
	return true
}

func readBMFFBox(data []byte, off int) (typ string, hdrSize, boxEnd int, ok bool) {
	if off < 0 || off+8 > len(data) {
		return "", 0, 0, false
	}
	size32 := binary.BigEndian.Uint32(data[off : off+4])
	typ = string(data[off+4 : off+8])
	switch size32 {
	case 0:
		hdrSize = 8
		boxEnd = len(data)
	case 1:
		if off+16 > len(data) {
			return "", 0, 0, false
		}
		size64 := binary.BigEndian.Uint64(data[off+8 : off+16])
		rem := len(data) - off
		if size64 < 16 || size64 > uint64(rem) { //nolint:gosec // G115: off <= len(data) verified above
			return "", 0, 0, false
		}
		hdrSize = 16
		boxEnd = off + int(size64) //nolint:gosec // G115: bounded <= len(data)-off
	default:
		if size32 < 8 || int(size32) > len(data)-off {
			return "", 0, 0, false
		}
		hdrSize = 8
		boxEnd = off + int(size32)
	}
	if off+hdrSize > boxEnd {
		return "", 0, 0, false
	}
	return typ, hdrSize, boxEnd, true
}

func findESDSInBoxes(data []byte, depth int) []byte {
	const maxBMFFDepth = 16
	if depth > maxBMFFDepth {
		return nil
	}
	for off := 0; off < len(data); {
		typ, hdrSize, boxEnd, ok := readBMFFBox(data, off)
		if !ok {
			return nil
		}
		payloadOff := off + hdrSize
		switch typ {
		case "esds":
			if payloadOff+4 <= boxEnd {
				return data[payloadOff+4 : boxEnd]
			}
			return nil
		case "moov", "trak", "mdia", "minf", "stbl":
			if found := findESDSInBoxes(data[payloadOff:boxEnd], depth+1); found != nil {
				return found
			}
		case "stsd":
			// FullBox (4B version/flags + 4B entry_count).
			if payloadOff+8 <= boxEnd {
				if found := findESDSInBoxes(data[payloadOff+8:boxEnd], depth+1); found != nil {
					return found
				}
			}
		case "mp4a", "enca":
			// AudioSampleEntry fixed header (28 bytes) precedes child boxes.
			if payloadOff+28 <= boxEnd {
				if found := findESDSInBoxes(data[payloadOff+28:boxEnd], depth+1); found != nil {
					return found
				}
			}
		}
		off = boxEnd
	}
	return nil
}

// ParseAudioConfig parses the AudioSpecificConfig from the init segment (ftyp + moov).
func ParseAudioConfig(initData []byte) (AudioConfig, error) {
	cfg := DefaultAudioConfig()
	esdsPayload := findESDSInBoxes(initData, 0)
	if esdsPayload == nil && len(initData) >= 8 && len(initData) <= 512 && bytes.HasPrefix(initData, []byte("esds")) {
		esdsPayload = initData[8:]
	}
	if len(esdsPayload) == 0 {
		return cfg, nil
	}
	if len(esdsPayload) > 4096 {
		esdsPayload = esdsPayload[:4096]
	}

	// Proper ISO/IEC 14496-1 descriptor walk:
	// ES_Descriptor (0x03) -> DecoderConfigDescriptor (0x04) -> DecSpecificInfo (0x05).
	if tag, _, off, ok := readDescriptorHeader(esdsPayload, 0); ok && tag == 0x03 {
		// ES_Descriptor header: ES_ID (2 bytes) + flags (1 byte)
		if off+3 <= len(esdsPayload) {
			flags := esdsPayload[off+2]
			off += 3
			if (flags & 0x80) != 0 { // streamDependenceFlag
				off += 2
			}
			if (flags & 0x40) != 0 { // URL_Flag
				if off < len(esdsPayload) {
					off += 1 + int(esdsPayload[off])
				}
			}
			if (flags & 0x20) != 0 { // OCRstreamFlag
				off += 2
			}
			if tag4, _, off4, ok4 := readDescriptorHeader(esdsPayload, off); ok4 && tag4 == 0x04 {
				// DecoderConfigDescriptor header: objectTypeIndication (1) + streamType (1) + bufferSizeDB (3) + maxBitrate (4) + avgBitrate (4) = 13 bytes
				off5 := off4 + 13
				if tag5, len5, ascOff, ok5 := readDescriptorHeader(esdsPayload, off5); ok5 && tag5 == 0x05 && ascOff+len5 <= len(esdsPayload) {
					if applyAudioSpecificConfig(esdsPayload[ascOff:ascOff+len5], &cfg) {
						return cfg, nil
					}
				}
			}
		}
	}

	// Fallback for synthetic/minimal esds payloads in unit tests that place tag 0x05 directly.
	if tag, length, ascOff, ok := readDescriptorHeader(esdsPayload, 0); ok && tag == 0x05 && ascOff+length <= len(esdsPayload) {
		applyAudioSpecificConfig(esdsPayload[ascOff:ascOff+length], &cfg)
	}

	return cfg, nil
}

// WriteADTSHeader writes a standard 7-byte ADTS packet header for an AAC
// frame into dst[:7] without allocating. dst must have length >= 7.
func WriteADTSHeader(dst []byte, aacFrameLen int, cfg AudioConfig) {
	_ = dst[6]
	packetLen := aacFrameLen + 7

	// Syncword: 12 bits 0xFFF
	dst[0] = 0xFF
	dst[1] = 0xF1 // MPEG-4, Layer 0, protection absent (no CRC)

	// Profile (2 bits), FreqIndex (4 bits), Private (1 bit), ChanConfig (3 bits)
	profile := (cfg.ObjectType - 1) & 0x03
	dst[2] = byte(((profile << 6) | ((cfg.FreqIndex & 0x0F) << 2) | ((cfg.ChanConfig >> 2) & 0x01)) & 0xFF) //nolint:gosec // G115: bitwise masked to 8 bits
	dst[3] = byte((((cfg.ChanConfig & 0x03) << 6) | ((packetLen >> 11) & 0x03)) & 0xFF)                     //nolint:gosec // G115: bitwise masked to 8 bits
	dst[4] = byte((packetLen >> 3) & 0xFF)                                                                  //nolint:gosec // G115: bitwise masked to 8 bits
	dst[5] = byte((((packetLen & 0x07) << 5) | 0x1F) & 0xFF)                                                //nolint:gosec // G115: bitwise masked to 8 bits
	dst[6] = 0xFC                                                                                           // Buffer fullness 0x7FF, number of AAC frames = 1 (index 0)
}

// MakeADTSHeader creates a standard 7-byte ADTS packet header for an AAC frame.
func MakeADTSHeader(aacFrameLen int, cfg AudioConfig) []byte {
	header := make([]byte, 7)
	WriteADTSHeader(header, aacFrameLen, cfg)
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

// EncryptedSample holds one encrypted sample's payload, IV, and CENC subsamples.
type EncryptedSample struct {
	Data       []byte
	IV         []byte
	Subsamples []Subsample
}

// DecryptFunc decrypts an audio sample buffer using AES-CTR (cenc).
type DecryptFunc func(kid, iv, input []byte, subsamples []Subsample) ([]byte, error)

// DecryptBatchFunc decrypts a slice of audio samples in a single call.
type DecryptBatchFunc func(kid []byte, samples []EncryptedSample) ([][]byte, error)

func appendADTSSample(out, decryptedSample []byte, cfg AudioConfig, sampleIdx int) ([]byte, error) {
	const maxADTSPayloadSize = 0x1FFF - 7
	if len(decryptedSample) > maxADTSPayloadSize {
		return nil, fmt.Errorf("sample %d size %d exceeds 13-bit ADTS frame limit (%d)", sampleIdx, len(decryptedSample), maxADTSPayloadSize)
	}
	hdrStart := len(out)
	out = append(out, 0, 0, 0, 0, 0, 0, 0)
	WriteADTSHeader(out[hdrStart:hdrStart+7], len(decryptedSample), cfg)
	return append(out, decryptedSample...), nil
}

func findChildBox(data []byte, target string) ([]byte, bool) {
	for off := 0; off < len(data); {
		typ, hdrSize, boxEnd, ok := readBMFFBox(data, off)
		if !ok {
			return nil, false
		}
		if typ == target {
			return data[off+hdrSize : boxEnd], true
		}
		off = boxEnd
	}
	return nil, false
}

type sampleEncSlot struct {
	iv       []byte
	subStart int
	subLen   int
}

type parseScratch struct {
	samplesEnc  []sampleEncSlot
	sampleSizes []int
	subPool     []Subsample
	samples     []EncryptedSample
}

var parseScratchPool = sync.Pool{
	New: func() any { return &parseScratch{} },
}

func getParseScratch() *parseScratch {
	return parseScratchPool.Get().(*parseScratch)
}

func putParseScratch(sc *parseScratch) {
	if sc == nil {
		return
	}
	const (
		maxPooledSamples = 8192
		maxPooledSubs    = 16384
	)
	clear(sc.samplesEnc)
	clear(sc.samples)
	if cap(sc.samplesEnc) > maxPooledSamples {
		sc.samplesEnc = nil
	} else {
		sc.samplesEnc = sc.samplesEnc[:0]
	}
	if cap(sc.sampleSizes) > maxPooledSamples {
		sc.sampleSizes = nil
	} else {
		sc.sampleSizes = sc.sampleSizes[:0]
	}
	if cap(sc.samples) > maxPooledSamples {
		sc.samples = nil
	} else {
		sc.samples = sc.samples[:0]
	}
	if cap(sc.subPool) > maxPooledSubs {
		sc.subPool = nil
	} else {
		sc.subPool = sc.subPool[:0]
	}
	parseScratchPool.Put(sc)
}

func parseSegmentSamples(segData []byte, sc *parseScratch) ([]EncryptedSample, int, error) {
	if len(segData) < 8 {
		return nil, 0, fmt.Errorf("segment too short")
	}

	var (
		hasMoof          bool
		moofStart        int
		moofPayload      []byte
		hasMdat          bool
		mdatPayloadStart int
		mdatEnd          int
	)
	for off := 0; off < len(segData); {
		typ, hdrSize, boxEnd, ok := readBMFFBox(segData, off)
		if !ok {
			return nil, 0, fmt.Errorf("malformed MP4 box at offset %d", off)
		}
		switch typ {
		case "moof":
			if !hasMoof {
				hasMoof = true
				moofStart = off
				moofPayload = segData[off+hdrSize : boxEnd]
			}
		case "mdat":
			if !hasMdat {
				hasMdat = true
				mdatPayloadStart = off + hdrSize
				mdatEnd = boxEnd
			}
		}
		off = boxEnd
	}
	if !hasMoof {
		return nil, 0, fmt.Errorf("moof box not found in segment")
	}

	trafPayload, ok := findChildBox(moofPayload, "traf")
	if !ok {
		return nil, 0, fmt.Errorf("traf box not found in moof")
	}

	var sencBody, trunBody []byte
	var hasSenc, hasTrun bool
	for off := 0; off < len(trafPayload); {
		typ, hdrSize, boxEnd, boxOK := readBMFFBox(trafPayload, off)
		if !boxOK {
			return nil, 0, fmt.Errorf("malformed box inside traf at offset %d", off)
		}
		switch typ {
		case "senc":
			if !hasSenc {
				hasSenc = true
				sencBody = trafPayload[off+hdrSize : boxEnd]
			}
		case "trun":
			if !hasTrun {
				hasTrun = true
				trunBody = trafPayload[off+hdrSize : boxEnd]
			}
		}
		off = boxEnd
	}

	// 1. Parse senc box
	if !hasSenc {
		return nil, 0, fmt.Errorf("senc box not found in moof")
	}
	if len(sencBody) < 8 {
		return nil, 0, fmt.Errorf("truncated senc box: need at least 8 payload bytes, got %d", len(sencBody))
	}
	flags := binary.BigEndian.Uint32(sencBody[0:4]) & 0x00FFFFFF
	hasSubsamples := (flags & 0x02) != 0
	sampleCount := binary.BigEndian.Uint32(sencBody[4:8])

	// Sanity-bound sampleCount against remaining senc buffer to prevent OOM
	sencEntries := sencBody[8:]
	if int64(sampleCount) > int64(len(sencEntries))/8 || sampleCount > 65536 {
		return nil, 0, fmt.Errorf("invalid or excessive sample count %d in senc (available bytes: %d)", sampleCount, len(sencEntries))
	}

	nSamples := int(sampleCount)
	if cap(sc.samplesEnc) < nSamples {
		sc.samplesEnc = make([]sampleEncSlot, nSamples)
	} else {
		sc.samplesEnc = sc.samplesEnc[:nSamples]
	}
	samplesEnc := sc.samplesEnc
	subPool := sc.subPool[:0]
	if hasSubsamples && nSamples > 0 && cap(subPool) < nSamples {
		subPool = make([]Subsample, 0, nSamples)
	}
	offset := 0
	ivSize := 8
	if !hasSubsamples && sampleCount > 0 {
		switch len(sencEntries) {
		case nSamples * 8:
			ivSize = 8
		case nSamples * 16:
			ivSize = 16
		default:
			return nil, 0, fmt.Errorf("invalid senc length %d for %d samples", len(sencEntries), sampleCount)
		}
	}
	for i := range sampleCount {
		if offset+ivSize > len(sencEntries) {
			return nil, 0, fmt.Errorf("unexpected EOF in senc IVs at sample %d", i)
		}
		iv := sencEntries[offset : offset+ivSize]
		offset += ivSize

		subStart := len(subPool)
		subLen := 0
		if hasSubsamples {
			if offset+2 > len(sencEntries) {
				return nil, 0, fmt.Errorf("unexpected EOF in senc subsamples count at sample %d", i)
			}
			subCount := binary.BigEndian.Uint16(sencEntries[offset : offset+2])
			offset += 2
			remainingSubBytes := len(sencEntries) - offset
			if int(subCount) > remainingSubBytes/6 {
				return nil, 0, fmt.Errorf("invalid subsample count %d at sample %d", subCount, i)
			}
			subLen = int(subCount)
			for range subCount {
				if offset+6 > len(sencEntries) {
					return nil, 0, fmt.Errorf("unexpected EOF in senc subsample entry")
				}
				clearBytes := binary.BigEndian.Uint16(sencEntries[offset : offset+2])
				cipherBytes := binary.BigEndian.Uint32(sencEntries[offset+2 : offset+6])
				offset += 6
				subPool = append(subPool, Subsample{
					ClearBytes:  clearBytes,
					CipherBytes: cipherBytes,
				})
			}
		}
		samplesEnc[i] = sampleEncSlot{
			iv:       iv,
			subStart: subStart,
			subLen:   subLen,
		}
	}
	sc.subPool = subPool
	if offset != len(sencEntries) {
		return nil, 0, fmt.Errorf("trailing bytes in senc box: consumed %d of %d", offset, len(sencEntries))
	}

	// 2. Parse trun box
	if !hasTrun {
		return nil, 0, fmt.Errorf("trun box not found in moof")
	}
	if len(trunBody) < 8 {
		return nil, 0, fmt.Errorf("truncated trun box: need at least 8 payload bytes, got %d", len(trunBody))
	}
	trunFlags := binary.BigEndian.Uint32(trunBody[0:4]) & 0x00FFFFFF
	trunSampleCount := binary.BigEndian.Uint32(trunBody[4:8])
	if trunSampleCount != sampleCount {
		return nil, 0, fmt.Errorf("sample count mismatch between senc (%d) and trun (%d)", sampleCount, trunSampleCount)
	}
	trunOffset := 8

	moofTotalLen := len(moofPayload) + 8
	dataOffset := int64(moofTotalLen + 8) // Default fallback to right after moof + mdat header
	if (trunFlags & 0x01) != 0 {
		if trunOffset+4 > len(trunBody) {
			return nil, 0, fmt.Errorf("truncated trun data_offset")
		}
		// data_offset is a signed 32-bit integer per ISO/IEC 14496-12
		dataOffset = int64(int32(binary.BigEndian.Uint32(trunBody[trunOffset : trunOffset+4]))) //nolint:gosec // G115: ISO/IEC 14496-12 defines data_offset as signed 32-bit
		trunOffset += 4
	}
	if (trunFlags & 0x04) != 0 {
		if trunOffset+4 > len(trunBody) {
			return nil, 0, fmt.Errorf("truncated trun first_sample_flags")
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

	if int64(sampleCount)*int64(entrySize) > int64(len(trunBody)-trunOffset) {
		return nil, 0, fmt.Errorf("truncated trun sample entries")
	}

	const maxSampleSize = 16 << 20 // 16 MiB defensive cap per audio sample
	if cap(sc.sampleSizes) < nSamples {
		sc.sampleSizes = make([]int, nSamples)
	} else {
		sc.sampleSizes = sc.sampleSizes[:nSamples]
	}
	sampleSizes := sc.sampleSizes
	if !hasSize {
		clear(sampleSizes)
	}
	var totalSampleBytes int
	for i := range sampleCount {
		if hasDuration {
			trunOffset += 4
		}
		if hasSize {
			szUint := binary.BigEndian.Uint32(trunBody[trunOffset : trunOffset+4])
			if szUint > maxSampleSize {
				return nil, 0, fmt.Errorf("excessive sample %d size %d in trun", i, szUint)
			}
			sz := int(szUint)
			sampleSizes[i] = sz
			if sz > 0 && totalSampleBytes < len(segData) {
				totalSampleBytes += sz
			}
			trunOffset += 4
		}
		if hasFlags {
			trunOffset += 4
		}
		if hasCompTime {
			trunOffset += 4
		}
	}

	// 3. Locate mdat and slice samples
	mdatTarget := int64(moofStart) + dataOffset
	if !hasMdat {
		return nil, 0, fmt.Errorf("mdat box not found in segment")
	}
	if mdatTarget < int64(mdatPayloadStart) || mdatTarget > int64(mdatEnd) {
		return nil, 0, fmt.Errorf("invalid dataOffset %d: points to %d (mdat payload [%d, %d])", dataOffset, mdatTarget, mdatPayloadStart, mdatEnd)
	}
	mdatData := segData[mdatTarget:mdatEnd]
	if cap(sc.samples) < nSamples {
		sc.samples = make([]EncryptedSample, nSamples)
	} else {
		sc.samples = sc.samples[:nSamples]
	}
	samples := sc.samples
	currMdatOffset := 0

	for i := range nSamples {
		sSize := sampleSizes[i]
		if sSize < 0 || sSize > len(mdatData)-currMdatOffset {
			return nil, 0, fmt.Errorf("sample %d exceeds mdat data bounds (size %d, remaining %d)", i, sSize, len(mdatData)-currMdatOffset)
		}
		encInfo := samplesEnc[i]
		var subs []Subsample
		if encInfo.subLen > 0 {
			subs = subPool[encInfo.subStart : encInfo.subStart+encInfo.subLen]
		}
		samples[i] = EncryptedSample{
			Data:       mdatData[currMdatOffset : currMdatOffset+sSize],
			IV:         encInfo.iv,
			Subsamples: subs,
		}
		currMdatOffset += sSize
	}

	return samples, min(totalSampleBytes, len(mdatData)) + nSamples*7, nil
}

func growOutputBuf(dst []byte, estBytes int) []byte {
	out := dst[:0]
	if cap(out) < estBytes {
		out = make([]byte, 0, estBytes)
	}
	return out
}

// DecryptSegment parses a fragmented MP4 audio segment (moof + mdat) and uses
// the provided decrypt function to decrypt samples, returning an elementary AAC bitstream.
func DecryptSegment(
	segData []byte,
	kid []byte,
	cfg AudioConfig,
	decryptFn DecryptFunc,
) ([]byte, error) {
	return DecryptSegmentInto(nil, segData, kid, cfg, decryptFn)
}

// DecryptSegmentInto behaves like DecryptSegment but writes the ADTS-framed
// output into dst (reusing its backing array when capacity permits).
func DecryptSegmentInto(
	dst []byte,
	segData []byte,
	kid []byte,
	cfg AudioConfig,
	decryptFn DecryptFunc,
) ([]byte, error) {
	sc := getParseScratch()
	defer putParseScratch(sc)

	samples, estBytes, err := parseSegmentSamples(segData, sc)
	if err != nil {
		return nil, err
	}
	out := growOutputBuf(dst, estBytes)
	for i, s := range samples {
		decryptedSample, err := decryptFn(kid, s.IV, s.Data, s.Subsamples)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt sample %d: %w", i, err)
		}
		out, err = appendADTSSample(out, decryptedSample, cfg, i)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DecryptSegmentBatch parses a fragmented MP4 audio segment (moof + mdat) and
// decrypts all samples in a single batch call before ADTS framing.
func DecryptSegmentBatch(
	segData []byte,
	kid []byte,
	cfg AudioConfig,
	decryptBatchFn DecryptBatchFunc,
) ([]byte, error) {
	return DecryptSegmentBatchInto(nil, segData, kid, cfg, decryptBatchFn)
}

// DecryptSegmentBatchInto behaves like DecryptSegmentBatch but writes the
// ADTS-framed output into dst (reusing its backing array when capacity permits).
func DecryptSegmentBatchInto(
	dst []byte,
	segData []byte,
	kid []byte,
	cfg AudioConfig,
	decryptBatchFn DecryptBatchFunc,
) ([]byte, error) {
	sc := getParseScratch()
	defer putParseScratch(sc)

	samples, estBytes, err := parseSegmentSamples(segData, sc)
	if err != nil {
		return nil, err
	}
	decryptedSamples, err := decryptBatchFn(kid, samples)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt segment batch: %w", err)
	}
	if len(decryptedSamples) != len(samples) {
		return nil, fmt.Errorf("batch decrypt returned %d samples, want %d", len(decryptedSamples), len(samples))
	}
	out := growOutputBuf(dst, estBytes)
	for i, decryptedSample := range decryptedSamples {
		out, err = appendADTSSample(out, decryptedSample, cfg, i)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
