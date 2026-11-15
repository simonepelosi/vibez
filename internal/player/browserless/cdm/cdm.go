//go:build (linux || darwin || windows) && cgo

package cdm

/*
#cgo linux LDFLAGS: -ldl -lstdc++
#cgo darwin LDFLAGS: -ldl -lc++
#cgo windows LDFLAGS: -static -static-libgcc -static-libstdc++
#include "cdm_bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

const (
	maxCDMInputBytes      = 64 << 20 // 64 MiB defensive CGo ceiling matching BufferImpl
	maxCDMSubsamples      = 65536
	maxCDMBatchSamples    = 65536
	maxCDMBatchSubsamples = 1 << 20
)

type CDM struct {
	mu      sync.Mutex
	ctx     *C.cdm_context_t
	certSet bool
}

// New loads the Widevine CDM shared library from the given path.
func New(libraryPath string) (*CDM, error) {
	if libraryPath == "" || strings.ContainsRune(libraryPath, 0) || strings.HasPrefix(libraryPath, `\\.\`) || !filepath.IsAbs(libraryPath) {
		return nil, errors.New("invalid Widevine CDM path")
	}
	info, err := os.Stat(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("invalid Widevine CDM path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("widevine CDM path %q is not a regular file", libraryPath)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o002 != 0 {
		return nil, fmt.Errorf("widevine CDM path %q must not be world-writable", libraryPath)
	}

	cPath := C.CString(libraryPath)
	defer C.free(unsafe.Pointer(cPath))

	ctx := C.cdm_context_create(cPath)
	if ctx == nil {
		return nil, fmt.Errorf("failed to initialize Widevine CDM from %s", libraryPath)
	}

	return &CDM{ctx: ctx}, nil
}

// SetServerCertificate sets the Widevine server certificate obtained from Apple.
func (c *CDM) SetServerCertificate(cert []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(cert) == 0 || len(cert) > maxCDMInputBytes {
		return errors.New("invalid server certificate size")
	}
	if c.ctx == nil {
		return errors.New("cdm context is closed")
	}
	if c.certSet {
		return nil
	}

	ret := C.cdm_context_set_server_certificate(
		c.ctx,
		(*C.uint8_t)(unsafe.Pointer(&cert[0])),
		C.uint32_t(len(cert)), //nolint:gosec // bounded <= maxCDMInputBytes
	)
	if ret != 0 {
		return fmt.Errorf("failed to set server certificate: %d", ret)
	}
	c.certSet = true
	return nil
}

// GenerateChallenge creates a playback session for the given 16-byte Key ID (KID)
// and returns the raw binary license challenge and the assigned session ID.
//
//nolint:gocritic // CGo-generated wrapper triggers false-positive dupSubExpr
func (c *CDM) GenerateChallenge(kid []byte) ([]byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(kid) != 16 {
		return nil, "", fmt.Errorf("invalid KID length: %d (expected 16)", len(kid))
	}
	if c.ctx == nil {
		return nil, "", errors.New("cdm context is closed")
	}

	var outChallenge *C.uint8_t
	var outChallengeSize C.uint32_t
	var outSessionID *C.char

	ret := C.cdm_context_generate_challenge(
		c.ctx,
		(*C.uint8_t)(unsafe.Pointer(&kid[0])),
		C.uint32_t(len(kid)),
		&outChallenge,
		&outChallengeSize,
		&outSessionID,
	)
	if ret != 0 {
		return nil, "", fmt.Errorf("failed to generate license challenge: %d", ret)
	}
	defer C.free(unsafe.Pointer(outChallenge))
	defer C.free(unsafe.Pointer(outSessionID))

	if outChallenge == nil || outSessionID == nil || outChallengeSize == 0 || uint64(outChallengeSize) > math.MaxInt32 {
		return nil, "", fmt.Errorf("invalid license challenge output size: %d", outChallengeSize)
	}

	challenge := C.GoBytes(unsafe.Pointer(outChallenge), C.int(outChallengeSize))
	sessionID := C.GoString(outSessionID)

	return challenge, sessionID, nil
}

// UpdateSession updates the session with the license payload returned from Apple.
func (c *CDM) UpdateSession(sessionID string, license []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if sessionID == "" || len(sessionID) > 4096 || strings.ContainsRune(sessionID, 0) || len(license) == 0 || len(license) > maxCDMInputBytes {
		return errors.New("invalid session update payload")
	}
	if c.ctx == nil {
		return errors.New("cdm context is closed")
	}

	cSessionID := C.CString(sessionID)
	defer C.free(unsafe.Pointer(cSessionID))

	ret := C.cdm_context_update_session(
		c.ctx,
		cSessionID,
		(*C.uint8_t)(unsafe.Pointer(&license[0])),
		C.uint32_t(len(license)), //nolint:gosec // bounded <= maxCDMInputBytes
	)
	if ret != 0 {
		return fmt.Errorf("failed to update session with license: %d", ret)
	}
	return nil
}

// CloseSession closes a Widevine session and releases any key state held by the CDM.
func (c *CDM) CloseSession(sessionID string) error {
	if c == nil || sessionID == "" {
		return nil
	}
	if len(sessionID) > 4096 || strings.ContainsRune(sessionID, 0) {
		return errors.New("invalid session ID")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return errors.New("cdm context is closed")
	}

	cSessionID := C.CString(sessionID)
	defer C.free(unsafe.Pointer(cSessionID))

	if ret := C.cdm_context_close_session(c.ctx, cSessionID); ret != 0 {
		return fmt.Errorf("failed to close session: %d", ret)
	}
	return nil
}

// Subsample specifies the clear and encrypted byte counts in an audio sample.
type Subsample struct {
	ClearBytes  uint16
	CipherBytes uint32
}

// DecryptScratch holds reusable per-segment CGo and output buffers for
// DecryptSubsamplesReuse and DecryptBatch, avoiding per-sample heap allocations.
type DecryptScratch struct {
	out      []byte
	clear    []C.uint16_t
	cipher   []C.uint32_t
	ivs      []byte
	in       []byte
	ivSizes  []C.uint32_t
	inSizes  []C.uint32_t
	subCnts  []C.uint32_t
	outSizes []C.uint32_t
	slices   [][]byte
}

// BatchSample describes one sample to decrypt in a DecryptBatch call.
type BatchSample struct {
	IV         []byte
	Input      []byte
	Subsamples []Subsample
}

// Decrypt decrypts an encrypted audio frame using AES-CTR (cenc).
func (c *CDM) Decrypt(keyID, iv, ciphertext []byte) ([]byte, error) {
	return c.DecryptSubsamples(keyID, iv, ciphertext, nil)
}

// DecryptSubsamples decrypts an audio frame with a subsample table using AES-CTR (cenc).
func (c *CDM) DecryptSubsamples(keyID, iv, input []byte, subsamples []Subsample) ([]byte, error) {
	return c.DecryptSubsamplesReuse(&DecryptScratch{}, keyID, iv, input, subsamples)
}

// DecryptSubsamplesReuse decrypts an audio frame into reusable scratch buffers.
// The returned slice aliases scratch's internal buffer and remains valid until
// the next DecryptSubsamplesReuse call using the same scratch.
func (c *CDM) DecryptSubsamplesReuse(scratch *DecryptScratch, keyID, iv, input []byte, subsamples []Subsample) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(keyID) == 0 || len(keyID) > maxCDMInputBytes || len(iv) == 0 || len(iv) > maxCDMInputBytes {
		return nil, errors.New("invalid decrypt input parameters")
	}
	if len(input) == 0 {
		return []byte{}, nil
	}
	if len(input) > maxCDMInputBytes {
		return nil, fmt.Errorf("decrypt input size %d exceeds maximum (%d)", len(input), maxCDMInputBytes)
	}
	if len(subsamples) > maxCDMSubsamples {
		return nil, fmt.Errorf("excessive subsample count: %d", len(subsamples))
	}
	if len(subsamples) > 0 {
		var total uint64
		for _, sub := range subsamples {
			total += uint64(sub.ClearBytes) + uint64(sub.CipherBytes)
			if total > uint64(len(input)) {
				return nil, fmt.Errorf("subsample byte sum exceeds input size (%d)", len(input))
			}
		}
	}
	if c.ctx == nil {
		return nil, errors.New("cdm context is closed")
	}

	if scratch == nil {
		scratch = &DecryptScratch{}
	}
	if cap(scratch.out) < len(input) {
		scratch.out = make([]byte, len(input))
	}
	decrypted := scratch.out[:len(input)]
	var outSize C.uint32_t

	var clearPtr *C.uint16_t
	var cipherPtr *C.uint32_t
	numSubs := C.uint32_t(len(subsamples)) //nolint:gosec // bounded <= maxCDMSubsamples
	if len(subsamples) > 0 {
		if cap(scratch.clear) < len(subsamples) {
			scratch.clear = make([]C.uint16_t, len(subsamples))
			scratch.cipher = make([]C.uint32_t, len(subsamples))
		}
		clearBytes := scratch.clear[:len(subsamples)]
		cipherBytes := scratch.cipher[:len(subsamples)]
		for i, sub := range subsamples {
			clearBytes[i] = C.uint16_t(sub.ClearBytes)
			cipherBytes[i] = C.uint32_t(sub.CipherBytes)
		}
		clearPtr = &clearBytes[0]
		cipherPtr = &cipherBytes[0]
	}

	ret := C.cdm_context_decrypt(
		c.ctx,
		(*C.uint8_t)(unsafe.Pointer(&keyID[0])),
		C.uint32_t(len(keyID)), //nolint:gosec // bounded <= maxCDMInputBytes
		(*C.uint8_t)(unsafe.Pointer(&iv[0])),
		C.uint32_t(len(iv)), //nolint:gosec // bounded <= maxCDMInputBytes
		(*C.uint8_t)(unsafe.Pointer(&input[0])),
		C.uint32_t(len(input)), //nolint:gosec // bounded <= maxCDMInputBytes
		clearPtr,
		cipherPtr,
		numSubs,
		(*C.uint8_t)(unsafe.Pointer(&decrypted[0])),
		&outSize,
	)
	if ret != 0 {
		return nil, fmt.Errorf("decryption failed with status %d", ret)
	}
	if uint64(outSize) > uint64(len(decrypted)) {
		return nil, fmt.Errorf("decryption output size %d exceeds buffer %d", outSize, len(decrypted))
	}

	return decrypted[:outSize], nil
}

// DecryptBatch decrypts all samples in a single lock/CGo round-trip using
// reusable scratch buffers. Returned slices alias scratch's internal buffer and
// remain valid until the next Decrypt* call with the same scratch.
func (c *CDM) DecryptBatch(scratch *DecryptScratch, keyID []byte, samples []BatchSample) ([][]byte, error) {
	if c == nil {
		return nil, errors.New("cdm context is closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(keyID) == 0 || len(keyID) > maxCDMInputBytes {
		return nil, errors.New("invalid decrypt input parameters")
	}
	if len(samples) > maxCDMBatchSamples {
		return nil, fmt.Errorf("excessive batch sample count: %d", len(samples))
	}
	if len(samples) == 0 {
		return nil, nil
	}

	var totalIVs, totalIn, totalSubs uint64
	for i, s := range samples {
		if len(s.IV) == 0 || len(s.IV) > maxCDMInputBytes {
			return nil, errors.New("invalid decrypt input parameters")
		}
		if len(s.Input) > maxCDMInputBytes {
			return nil, fmt.Errorf("decrypt input size %d exceeds maximum (%d)", len(s.Input), maxCDMInputBytes)
		}
		if len(s.Subsamples) > maxCDMSubsamples {
			return nil, fmt.Errorf("excessive subsample count: %d", len(s.Subsamples))
		}
		var subTotal uint64
		for _, sub := range s.Subsamples {
			subTotal += uint64(sub.ClearBytes) + uint64(sub.CipherBytes)
			if subTotal > uint64(len(s.Input)) {
				return nil, fmt.Errorf("subsample byte sum exceeds input size (%d) at sample %d", len(s.Input), i)
			}
		}
		totalIVs += uint64(len(s.IV))
		totalIn += uint64(len(s.Input))
		totalSubs += uint64(len(s.Subsamples))
		if totalIVs > maxCDMInputBytes || totalIn > maxCDMInputBytes {
			return nil, fmt.Errorf("batch decrypt total bytes exceeds maximum (%d)", maxCDMInputBytes)
		}
		if totalSubs > maxCDMBatchSubsamples {
			return nil, fmt.Errorf("excessive total subsamples: %d", totalSubs)
		}
	}
	if c.ctx == nil {
		return nil, errors.New("cdm context is closed")
	}

	if scratch == nil {
		scratch = &DecryptScratch{}
	}
	n := len(samples)
	if cap(scratch.slices) < n {
		scratch.slices = make([][]byte, n)
	}
	outSlices := scratch.slices[:n]
	if totalIn == 0 {
		clear(outSlices)
		return outSlices, nil
	}

	scratch.ivs = scratch.ivs[:0]
	scratch.in = scratch.in[:0]
	if cap(scratch.ivSizes) < n {
		scratch.ivSizes = make([]C.uint32_t, n)
		scratch.inSizes = make([]C.uint32_t, n)
		scratch.subCnts = make([]C.uint32_t, n)
		scratch.outSizes = make([]C.uint32_t, n)
	}
	ivSizes := scratch.ivSizes[:n]
	inSizes := scratch.inSizes[:n]
	subCnts := scratch.subCnts[:n]
	outSizes := scratch.outSizes[:n]

	scratch.clear = scratch.clear[:0]
	scratch.cipher = scratch.cipher[:0]
	if uint64(cap(scratch.out)) < totalIn {
		scratch.out = make([]byte, totalIn)
	}
	outBuf := scratch.out[:totalIn]

	for i, s := range samples {
		scratch.ivs = append(scratch.ivs, s.IV...)
		scratch.in = append(scratch.in, s.Input...)
		ivSizes[i] = C.uint32_t(len(s.IV))         //nolint:gosec // G115: bounded <= maxCDMInputBytes
		inSizes[i] = C.uint32_t(len(s.Input))      //nolint:gosec // G115: bounded <= maxCDMInputBytes
		subCnts[i] = C.uint32_t(len(s.Subsamples)) //nolint:gosec // G115: bounded <= maxCDMSubsamples
		for _, sub := range s.Subsamples {
			scratch.clear = append(scratch.clear, C.uint16_t(sub.ClearBytes))
			scratch.cipher = append(scratch.cipher, C.uint32_t(sub.CipherBytes))
		}
	}

	var clearPtr *C.uint16_t
	var cipherPtr *C.uint32_t
	if len(scratch.clear) > 0 {
		clearPtr = &scratch.clear[0]
		cipherPtr = &scratch.cipher[0]
	}

	ret := C.cdm_context_decrypt_batch(
		c.ctx,
		(*C.uint8_t)(unsafe.Pointer(&keyID[0])),
		C.uint32_t(len(keyID)), //nolint:gosec // G115: bounded <= maxCDMInputBytes
		(*C.uint8_t)(unsafe.Pointer(&scratch.ivs[0])),
		C.uint32_t(len(scratch.ivs)), //nolint:gosec // G115: bounded <= maxCDMInputBytes
		&ivSizes[0],
		(*C.uint8_t)(unsafe.Pointer(&scratch.in[0])),
		C.uint32_t(len(scratch.in)), //nolint:gosec // G115: bounded <= maxCDMInputBytes
		&inSizes[0],
		clearPtr,
		cipherPtr,
		&subCnts[0],
		C.uint32_t(len(scratch.clear)), //nolint:gosec // G115: bounded <= maxCDMBatchSubsamples
		C.uint32_t(n),                  //nolint:gosec // G115: bounded <= maxCDMBatchSamples
		(*C.uint8_t)(unsafe.Pointer(&outBuf[0])),
		&outSizes[0],
	)
	if ret != 0 {
		return nil, fmt.Errorf("batch decryption failed with status %d", ret)
	}

	var pos uint64
	for i := range n {
		sz := uint64(outSizes[i])
		if sz > uint64(inSizes[i]) || pos+sz > totalIn {
			return nil, fmt.Errorf("batch decryption output size %d exceeds buffer at sample %d", sz, i)
		}
		outSlices[i] = outBuf[pos : pos+sz]
		pos += sz
	}
	return outSlices, nil
}

// Close destroys the CDM instance and frees resources.
func (c *CDM) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		C.cdm_context_destroy(c.ctx)
		c.ctx = nil
	}
	return nil
}
