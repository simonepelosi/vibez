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
	"sync"
	"unsafe"
)

const (
	maxCDMInputBytes = 64 << 20 // 64 MiB defensive CGo ceiling matching BufferImpl
	maxCDMSubsamples = 65536
)

type CDM struct {
	mu      sync.Mutex
	ctx     *C.cdm_context_t
	certSet bool
}

// New loads the Widevine CDM shared library from the given path.
func New(libraryPath string) (*CDM, error) {
	if libraryPath == "" {
		return nil, errors.New("empty Widevine CDM path")
	}
	info, err := os.Stat(libraryPath)
	if err != nil {
		return nil, fmt.Errorf("invalid Widevine CDM path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("widevine CDM path %q is not a regular file", libraryPath)
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

	if sessionID == "" || len(license) == 0 || len(license) > maxCDMInputBytes {
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

// Subsample specifies the clear and encrypted byte counts in an audio sample.
type Subsample struct {
	ClearBytes  uint16
	CipherBytes uint32
}

// Decrypt decrypts an encrypted audio frame using AES-CTR (cenc).
func (c *CDM) Decrypt(keyID, iv, ciphertext []byte) ([]byte, error) {
	return c.DecryptSubsamples(keyID, iv, ciphertext, nil)
}

// DecryptSubsamples decrypts an audio frame with a subsample table using AES-CTR (cenc).
func (c *CDM) DecryptSubsamples(keyID, iv, input []byte, subsamples []Subsample) ([]byte, error) {
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

	decrypted := make([]byte, len(input))
	var outSize C.uint32_t

	var clearPtr *C.uint16_t
	var cipherPtr *C.uint32_t
	numSubs := C.uint32_t(len(subsamples)) //nolint:gosec // bounded <= maxCDMSubsamples
	if len(subsamples) > 0 {
		clearBytes := make([]C.uint16_t, len(subsamples))
		cipherBytes := make([]C.uint32_t, len(subsamples))
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
