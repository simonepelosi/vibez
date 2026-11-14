package browserless

import (
	"bytes"
	"encoding/binary"
	"time"
)

// rawFormat describes an unencrypted library upload as served by Apple: a
// plain file rather than an encrypted HLS stream.
type rawFormat struct {
	ext         string // stream URL extension, which the sinks key on
	contentType string
	// seekable is false when a byte range from the middle of the file cannot
	// be decoded on its own, as with MP4, whose index may sit at either end.
	seekable bool
	// duration is zero when the header does not say.
	duration time.Duration
}

// sniffRawFormat identifies an upload from the start of the file and its
// total size. ok is false for anything the sinks cannot play.
func sniffRawFormat(head []byte, size int64) (rawFormat, bool) {
	if len(head) >= 8 && string(head[4:8]) == "ftyp" {
		return rawFormat{ext: ".m4a", contentType: "audio/mp4"}, true
	}
	if d, ok := mp3Duration(head, size); ok {
		return rawFormat{ext: ".mp3", contentType: "audio/mpeg", seekable: true, duration: d}, true
	}
	return rawFormat{}, false
}

var (
	mp3BitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3BitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3Rates      = [3]int{44100, 48000, 32000}
)

// mp3Duration finds the first MPEG layer III frame after any ID3v2 tag and
// derives the track length: from the Xing/Info or VBRI frame count when
// present, otherwise from the first frame's bitrate, which is exact for CBR.
func mp3Duration(head []byte, size int64) (time.Duration, bool) {
	start := 0
	if len(head) >= 10 && string(head[:3]) == "ID3" {
		tag := int(head[6]&0x7f)<<21 | int(head[7]&0x7f)<<14 | int(head[8]&0x7f)<<7 | int(head[9]&0x7f)
		start = 10 + tag
		if head[5]&0x10 != 0 {
			start += 10
		}
	}
	for i := start; i+4 <= len(head); i++ {
		if head[i] != 0xff || head[i+1]&0xe0 != 0xe0 {
			continue
		}
		h := head[i : i+4]
		version := (h[1] >> 3) & 3 // 3 = MPEG-1, 2 = MPEG-2, 0 = MPEG-2.5
		layer := (h[1] >> 1) & 3   // 1 = layer III
		brIdx := h[2] >> 4
		srIdx := (h[2] >> 2) & 3
		if version == 1 || layer != 1 || brIdx == 0 || brIdx == 15 || srIdx == 3 {
			continue
		}
		rate := mp3Rates[srIdx]
		kbps := mp3BitratesV1[brIdx]
		samplesPerFrame := 1152
		if version != 3 {
			rate /= 2
			if version == 0 {
				rate /= 2
			}
			kbps = mp3BitratesV2[brIdx]
			samplesPerFrame = 576
		}
		mono := h[3]>>6 == 3

		// The Xing/Info tag follows the side information; VBRI sits at a
		// fixed 32 bytes past the header.
		side := 32
		switch {
		case version == 3 && mono:
			side = 17
		case version != 3 && !mono:
			side = 17
		case version != 3 && mono:
			side = 9
		}
		var frames uint64
		if x := i + 4 + side; x+12 <= len(head) && (bytes.Equal(head[x:x+4], []byte("Xing")) || bytes.Equal(head[x:x+4], []byte("Info"))) {
			if binary.BigEndian.Uint32(head[x+4:x+8])&1 != 0 {
				frames = uint64(binary.BigEndian.Uint32(head[x+8 : x+12]))
			}
		} else if v := i + 4 + 32; v+18 <= len(head) && bytes.Equal(head[v:v+4], []byte("VBRI")) {
			frames = uint64(binary.BigEndian.Uint32(head[v+14 : v+18]))
		}
		const maxDurationSec = 100 * 365 * 24 * 3600 // fits safely within int64 nanoseconds (~292 years max)
		if rate <= 0 {
			return 0, false
		}
		rateU := uint64(rate)
		if frames > 0 {
			totalSamples := frames * uint64(samplesPerFrame)
			sec := totalSamples / rateU
			rem := totalSamples % rateU
			if sec > maxDurationSec {
				return time.Duration(maxDurationSec) * time.Second, true
			}
			dur := time.Duration(sec)*time.Second + time.Duration((rem*uint64(time.Second))/rateU) //nolint:gosec // bounded by maxDurationSec
			return dur, true
		}
		audio := size - int64(i)
		if audio <= 0 {
			return 0, true
		}
		bps := int64(kbps * 1000)
		// Compute (audio * 8 * time.Second) / bps without overflowing int64 for multi-GB files.
		sec := (audio / bps) * 8
		if sec > maxDurationSec {
			return time.Duration(maxDurationSec) * time.Second, true
		}
		remBytes := audio % bps
		remDur := ((remBytes * 8) / bps) * int64(time.Second)
		remFrac := (((remBytes * 8) % bps) * int64(time.Second)) / bps
		return time.Duration(sec)*time.Second + time.Duration(remDur+remFrac), true
	}
	return 0, false
}
