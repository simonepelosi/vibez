package local

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"
)

func buildVorbisCommentPayload(vendor string, comments []string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(vendor))) //nolint:gosec // test fixture
	b.WriteString(vendor)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(comments))) //nolint:gosec // test fixture
	for _, c := range comments {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(c))) //nolint:gosec // test fixture
		b.WriteString(c)
	}
	return b.Bytes()
}

func buildFLACBlock(typ byte, last bool, payload []byte) []byte {
	var b bytes.Buffer
	head := typ & 0x7F
	if last {
		head |= 0x80
	}
	l := len(payload)
	b.Write([]byte{head, byte(l >> 16), byte(l >> 8), byte(l)}) //nolint:gosec // test fixture
	b.Write(payload)
	return b.Bytes()
}

func buildPictureBlockPayload(picType uint32, mime, desc string, claimedDataLen uint32, actualData []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, picType)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(mime))) //nolint:gosec // test fixture
	b.WriteString(mime)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(desc))) //nolint:gosec // test fixture
	b.WriteString(desc)
	_ = binary.Write(&b, binary.BigEndian, uint32(1)) // width
	_ = binary.Write(&b, binary.BigEndian, uint32(1)) // height
	_ = binary.Write(&b, binary.BigEndian, uint32(24))
	_ = binary.Write(&b, binary.BigEndian, uint32(0))
	_ = binary.Write(&b, binary.BigEndian, claimedDataLen)
	b.Write(actualData)
	return b.Bytes()
}

func oggPageCRC(data []byte) uint32 {
	const poly = 0x04c11db7
	var crc uint32
	for _, v := range data {
		crc ^= uint32(v) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func buildSinglePacketOGG(serial, seq uint32, payload []byte) []byte {
	var hdr [27]byte
	copy(hdr[0:4], "OggS")
	hdr[4] = 0 // version
	hdr[5] = 0 // flags
	binary.LittleEndian.PutUint32(hdr[14:18], serial)
	binary.LittleEndian.PutUint32(hdr[18:22], seq)

	var segs []byte
	rem := len(payload)
	for rem >= 255 {
		segs = append(segs, 255)
		rem -= 255
	}
	segs = append(segs, byte(rem)) //nolint:gosec // rem < 255
	hdr[26] = byte(len(segs))      //nolint:gosec // test packet fits single page

	var page bytes.Buffer
	page.Write(hdr[:])
	page.Write(segs)
	page.Write(payload)
	raw := page.Bytes()
	crc := oggPageCRC(raw)
	binary.LittleEndian.PutUint32(raw[22:26], crc)
	return raw
}

func TestSafeReadTags_RejectsFLACAndOGGOOMBombs(t *testing.T) {
	badPic := buildPictureBlockPayload(3, "image/png", "", 0x7fffffff, []byte("tiny"))
	badComment := "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(badPic)

	t.Run("FLAC picture block dataLen bomb", func(t *testing.T) {
		var raw bytes.Buffer
		raw.WriteString("fLaC")
		raw.Write(buildFLACBlock(6, true, badPic))
		if _, err := safeReadTags(bytes.NewReader(raw.Bytes())); err == nil {
			t.Fatal("expected error for oversized FLAC picture block dataLen")
		}
	})

	t.Run("FLAC vorbis comment picture bomb", func(t *testing.T) {
		vc := buildVorbisCommentPayload("vibez", []string{badComment})
		var raw bytes.Buffer
		raw.WriteString("fLaC")
		raw.Write(buildFLACBlock(4, true, vc))
		if _, err := safeReadTags(bytes.NewReader(raw.Bytes())); err == nil {
			t.Fatal("expected error for oversized FLAC METADATA_BLOCK_PICTURE comment")
		}
	})

	t.Run("FLAC vorbis comment huge vendorLen", func(t *testing.T) {
		var vc bytes.Buffer
		_ = binary.Write(&vc, binary.LittleEndian, uint32(0x7fffffff))
		vc.WriteString("short")
		var raw bytes.Buffer
		raw.WriteString("fLaC")
		raw.Write(buildFLACBlock(4, true, vc.Bytes()))
		if _, err := safeReadTags(bytes.NewReader(raw.Bytes())); err == nil {
			t.Fatal("expected error for oversized FLAC vorbis vendorLen")
		}
	})

	t.Run("OGG Vorbis comment picture bomb", func(t *testing.T) {
		vc := buildVorbisCommentPayload("vibez", []string{"TITLE=Safe", badComment})
		pkt := append([]byte("\x03vorbis"), vc...)
		raw := buildSinglePacketOGG(1, 0, pkt)
		if _, err := safeReadTags(bytes.NewReader(raw)); err == nil {
			t.Fatal("expected error for oversized OGG Vorbis METADATA_BLOCK_PICTURE")
		}
	})

	t.Run("OGG OpusTags huge vendorLen", func(t *testing.T) {
		var opus bytes.Buffer
		opus.WriteString("OpusTags")
		_ = binary.Write(&opus, binary.LittleEndian, uint32(0x7fffffff))
		opus.WriteString("short")
		raw := buildSinglePacketOGG(2, 0, opus.Bytes())
		if _, err := safeReadTags(bytes.NewReader(raw)); err == nil {
			t.Fatal("expected error for oversized OGG OpusTags vendorLen")
		}
	})
}

func TestSafeReadTags_ValidFLACAndOGG(t *testing.T) {
	goodPic := buildPictureBlockPayload(3, "image/png", "cover", 4, []byte{1, 2, 3, 4})
	goodPicComment := "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(goodPic)

	vc := buildVorbisCommentPayload("vibez", []string{"TITLE=Song", "ARTIST=Artist", goodPicComment})
	var flac bytes.Buffer
	flac.WriteString("fLaC")
	flac.Write(buildFLACBlock(4, true, vc))
	m, err := safeReadTags(bytes.NewReader(flac.Bytes()))
	if err != nil {
		t.Fatalf("safeReadTags(valid FLAC) error: %v", err)
	}
	if m.Title() != "Song" || m.Artist() != "Artist" {
		t.Fatalf("FLAC tags = (%q, %q), want (Song, Artist)", m.Title(), m.Artist())
	}

	ogg := buildSinglePacketOGG(1, 0, append([]byte("\x03vorbis"), vc...))
	om, err := safeReadTags(bytes.NewReader(ogg))
	if err != nil {
		t.Fatalf("safeReadTags(valid OGG) error: %v", err)
	}
	if om.Title() != "Song" || om.Artist() != "Artist" {
		t.Fatalf("OGG tags = (%q, %q), want (Song, Artist)", om.Title(), om.Artist())
	}
}
