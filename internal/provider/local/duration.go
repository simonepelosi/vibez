package local

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// readFileDuration returns the audio duration of path by inspecting container
// headers. It returns 0 if the format is unrecognized or truncated.
func readFileDuration(f *os.File, path string) time.Duration {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return readFLACDuration(f)
	case ".wav":
		return readWAVDuration(f)
	case ".aiff", ".aif":
		return readAIFFDuration(f)
	case ".m4a":
		d, _ := readMP4Info(f)
		return d
	case ".mp3":
		return readMP3Duration(f)
	case ".ogg":
		return readOGGDuration(f)
	default:
		return 0
	}
}

// isLosslessM4A reports whether an .m4a file contains an Apple Lossless (alac)
// audio track rather than lossy AAC (mp4a).
func isLosslessM4A(path string) bool {
	f, err := os.Open(path) //nolint:gosec // user-configured music dir
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	_, codec := readMP4Info(f)
	return codec == "alac"
}

// skipID3v2 advances r past a leading ID3v2 tag if one is present.
func skipID3v2(r io.ReadSeeker) error {
	var hdr [10]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	if string(hdr[0:3]) != "ID3" {
		_, err := r.Seek(0, io.SeekStart)
		return err
	}
	size := int64(hdr[6]&0x7f)<<21 |
		int64(hdr[7]&0x7f)<<14 |
		int64(hdr[8]&0x7f)<<7 |
		int64(hdr[9]&0x7f)
	if hdr[5]&0x10 != 0 {
		size += 10 // footer present
	}
	_, err := r.Seek(10+size, io.SeekStart)
	return err
}

func readFLACDuration(r io.ReadSeeker) time.Duration {
	if err := skipID3v2(r); err != nil {
		return 0
	}
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil || string(magic[:]) != "fLaC" {
		return 0
	}
	for {
		var bh [4]byte
		if _, err := io.ReadFull(r, bh[:]); err != nil {
			return 0
		}
		last := (bh[0] & 0x80) != 0
		blockType := bh[0] & 0x7f
		length := int64(bh[1])<<16 | int64(bh[2])<<8 | int64(bh[3])
		if blockType == 0 && length >= 18 {
			var si [18]byte
			if _, err := io.ReadFull(r, si[:]); err != nil {
				return 0
			}
			v := binary.BigEndian.Uint64(si[10:18])
			sampleRate := (v >> 44) & 0xFFFFF
			totalSamples := v & 0xFFFFFFFFF
			if sampleRate == 0 || totalSamples == 0 {
				return 0
			}
			return time.Duration(totalSamples) * time.Second / time.Duration(sampleRate)
		}
		if last {
			return 0
		}
		if _, err := r.Seek(length, io.SeekCurrent); err != nil {
			return 0
		}
	}
}

func readWAVDuration(r io.ReadSeeker) time.Duration {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return 0
	}
	var byteRate uint32
	var dataSize uint32
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			break
		}
		id := string(ch[0:4])
		sz := binary.LittleEndian.Uint32(ch[4:8])
		switch id {
		case "fmt ":
			if sz >= 16 {
				var fmtBuf [16]byte
				if _, err := io.ReadFull(r, fmtBuf[:]); err != nil {
					return 0
				}
				byteRate = binary.LittleEndian.Uint32(fmtBuf[8:12])
				rem := int64(sz) - 16
				if sz%2 == 1 {
					rem++
				}
				if rem > 0 {
					if _, err := r.Seek(rem, io.SeekCurrent); err != nil {
						return 0
					}
				}
				continue
			}
		case "data":
			dataSize = sz
			if byteRate > 0 && dataSize > 0 {
				return time.Duration(dataSize) * time.Second / time.Duration(byteRate)
			}
		}
		skip := int64(sz)
		if skip%2 == 1 {
			skip++
		}
		if _, err := r.Seek(skip, io.SeekCurrent); err != nil {
			break
		}
	}
	if byteRate > 0 && dataSize > 0 {
		return time.Duration(dataSize) * time.Second / time.Duration(byteRate)
	}
	return 0
}

func readAIFFDuration(r io.ReadSeeker) time.Duration {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0
	}
	form := string(hdr[8:12])
	if string(hdr[0:4]) != "FORM" || (form != "AIFF" && form != "AIFC") {
		return 0
	}
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return 0
		}
		id := string(ch[0:4])
		sz := binary.BigEndian.Uint32(ch[4:8])
		if id == "COMM" && sz >= 18 {
			var comm [18]byte
			if _, err := io.ReadFull(r, comm[:]); err != nil {
				return 0
			}
			frames := binary.BigEndian.Uint32(comm[2:6])
			sr := parseIEEE80(comm[8:18])
			if frames == 0 || sr <= 0 {
				return 0
			}
			return time.Duration(float64(frames) / sr * float64(time.Second))
		}
		skip := int64(sz)
		if skip%2 == 1 {
			skip++
		}
		if _, err := r.Seek(skip, io.SeekCurrent); err != nil {
			return 0
		}
	}
}

// parseIEEE80 converts an 80-bit big-endian IEEE 754 extended float (used for
// sample rate in AIFF COMM chunks) to float64.
func parseIEEE80(b []byte) float64 {
	expon := int(binary.BigEndian.Uint16(b[0:2])) & 0x7FFF
	mant := binary.BigEndian.Uint64(b[2:10])
	if expon == 0 && mant == 0 {
		return 0
	}
	f := math.Ldexp(float64(mant), expon-16383-63)
	if b[0]&0x80 != 0 {
		f = -f
	}
	return f
}

// readMP4Info walks an ISO BMFF (.m4a) container to extract its duration from
// mvhd and its primary audio sample entry fourCC (e.g. "alac" or "mp4a") from
// stsd.
func readMP4Info(r io.ReadSeeker) (time.Duration, string) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil || end <= 8 {
		return 0, ""
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, ""
	}
	var dur time.Duration
	var codec string
	walkMP4Boxes(r, 0, end, &dur, &codec)
	return dur, codec
}

func walkMP4Boxes(r io.ReadSeeker, start, end int64, dur *time.Duration, codec *string) {
	pos := start
	for pos+8 <= end {
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return
		}
		var hdr [16]byte
		if _, err := io.ReadFull(r, hdr[:8]); err != nil {
			return
		}
		sz := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		hdrLen := int64(8)
		switch sz {
		case 1:
			if _, err := io.ReadFull(r, hdr[8:16]); err != nil {
				return
			}
			sz = int64(binary.BigEndian.Uint64(hdr[8:16])) //nolint:gosec
			hdrLen = 16
		case 0:
			sz = end - pos
		}
		if sz < hdrLen || pos+sz > end {
			return
		}
		bodyStart := pos + hdrLen
		bodyEnd := pos + sz

		switch typ {
		case "moov", "trak", "mdia", "minf", "stbl":
			walkMP4Boxes(r, bodyStart, bodyEnd, dur, codec)
		case "mvhd":
			if bodyEnd-bodyStart >= 20 {
				var buf [32]byte
				n := min(int(bodyEnd-bodyStart), len(buf))
				if _, err := io.ReadFull(r, buf[:n]); err == nil {
					version := buf[0]
					if version == 0 && n >= 20 {
						ts := binary.BigEndian.Uint32(buf[12:16])
						d := binary.BigEndian.Uint32(buf[16:20])
						if ts > 0 && d > 0 {
							*dur = time.Duration(d) * time.Second / time.Duration(ts)
						}
					} else if version == 1 && n >= 32 {
						ts := binary.BigEndian.Uint32(buf[20:24])
						d := binary.BigEndian.Uint64(buf[24:32])
						if ts > 0 && d > 0 {
							*dur = time.Duration(d) * time.Second / time.Duration(ts) //nolint:gosec
						}
					}
				}
			}
		case "stsd":
			// stsd fullbox: 4 bytes version/flags + 4 bytes entry_count +
			// first sample entry (4 bytes size + 4 bytes fourCC).
			if *codec == "" && bodyEnd-bodyStart >= 16 {
				var buf [16]byte
				if _, err := io.ReadFull(r, buf[:]); err == nil {
					fourCC := strings.ToLower(string(buf[12:16]))
					if fourCC == "alac" || fourCC == "mp4a" || fourCC == "flac" {
						*codec = fourCC
					}
				}
			}
		}
		pos = bodyEnd
	}
}

func readMP3Duration(r io.ReadSeeker) time.Duration {
	fileSize, err := r.Seek(0, io.SeekEnd)
	if err != nil || fileSize <= 0 {
		return 0
	}
	if err := skipID3v2(r); err != nil {
		return 0
	}
	audioStart, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0
	}
	var buf [4096]byte
	n, _ := r.Read(buf[:])
	if n < 4 {
		return 0
	}
	for i := 0; i+4 <= n; i++ {
		if buf[i] != 0xFF || (buf[i+1]&0xE0) != 0xE0 {
			continue
		}
		verBits := (buf[i+1] >> 3) & 0x03
		layerBits := (buf[i+1] >> 1) & 0x03
		brIdx := (buf[i+2] >> 4) & 0x0F
		srIdx := (buf[i+2] >> 2) & 0x03
		chMode := (buf[i+3] >> 6) & 0x03
		if verBits == 1 || layerBits != 1 || brIdx == 0 || brIdx == 15 || srIdx == 3 {
			continue
		}
		// Layer III tables.
		var sr int
		var spf int
		var sideInfo int
		switch verBits {
		case 3: // MPEG-1
			sr = [...]int{44100, 48000, 32000}[srIdx]
			spf = 1152
			if chMode == 3 {
				sideInfo = 17
			} else {
				sideInfo = 32
			}
		case 2: // MPEG-2
			sr = [...]int{22050, 24000, 16000}[srIdx]
			spf = 576
			if chMode == 3 {
				sideInfo = 9
			} else {
				sideInfo = 17
			}
		default: // MPEG-2.5
			sr = [...]int{11025, 12000, 8000}[srIdx]
			spf = 576
			if chMode == 3 {
				sideInfo = 9
			} else {
				sideInfo = 17
			}
		}
		var kbps int
		if verBits == 3 {
			kbps = [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}[brIdx]
		} else {
			kbps = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}[brIdx]
		}
		// Check Xing/Info VBR header.
		xingOff := i + 4 + sideInfo
		if xingOff+12 <= n {
			tag := string(buf[xingOff : xingOff+4])
			if tag == "Xing" || tag == "Info" {
				flags := binary.BigEndian.Uint32(buf[xingOff+4 : xingOff+8])
				if flags&0x01 != 0 {
					frames := binary.BigEndian.Uint32(buf[xingOff+8 : xingOff+12])
					if frames > 0 && sr > 0 {
						return time.Duration(frames) * time.Duration(spf) * time.Second / time.Duration(sr)
					}
				}
			}
		}
		if kbps > 0 && fileSize > audioStart {
			audioBytes := fileSize - audioStart
			return time.Duration(audioBytes*8) * time.Second / time.Duration(kbps*1000)
		}
		return 0
	}
	return 0
}

func readOGGDuration(r io.ReadSeeker) time.Duration {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil || end < 64 {
		return 0
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0
	}
	var head [256]byte
	n, _ := r.Read(head[:])
	var sampleRate uint32
	for i := 0; i+16 <= n; i++ {
		if head[i] == 0x01 && string(head[i+1:i+7]) == "vorbis" {
			sampleRate = binary.LittleEndian.Uint32(head[i+11 : i+15])
			break
		}
	}
	if sampleRate == 0 {
		return 0
	}
	tailSize := min(end, int64(65536))
	if _, err := r.Seek(end-tailSize, io.SeekStart); err != nil {
		return 0
	}
	tail := make([]byte, tailSize)
	tn, _ := io.ReadFull(r, tail)
	for i := tn - 14; i >= 0; i-- {
		if string(tail[i:i+4]) == "OggS" {
			granule := binary.LittleEndian.Uint64(tail[i+6 : i+14])
			if granule > 0 && granule != math.MaxUint64 {
				return time.Duration(granule) * time.Second / time.Duration(sampleRate) //nolint:gosec
			}
		}
	}
	return 0
}
