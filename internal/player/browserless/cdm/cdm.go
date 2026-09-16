//go:build linux

package cdm

/*
#cgo LDFLAGS: -ldl -lstdc++
#include "cdm_bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

type CDM struct {
	mu  sync.Mutex
	ctx *C.cdm_context_t
}

// New loads the Widevine CDM shared library from the given path.
func New(libraryPath string) (*CDM, error) {
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

	if c.ctx == nil {
		return errors.New("cdm context is closed")
	}

	ret := C.cdm_context_set_server_certificate(
		c.ctx,
		(*C.uint8_t)(unsafe.Pointer(&cert[0])),
		C.uint32_t(len(cert)),
	)
	if ret != 0 {
		return fmt.Errorf("failed to set server certificate: %d", ret)
	}
	return nil
}

// GenerateChallenge creates a playback session for the given 16-byte Key ID (KID)
// and returns the raw binary license challenge and the assigned session ID.
//
//nolint:gocritic // CGo-generated wrapper triggers false-positive dupSubExpr
func (c *CDM) GenerateChallenge(kid []byte) ([]byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return nil, "", errors.New("cdm context is closed")
	}
	if len(kid) != 16 {
		return nil, "", fmt.Errorf("invalid KID length: %d (expected 16)", len(kid))
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

	challenge := C.GoBytes(unsafe.Pointer(outChallenge), C.int(outChallengeSize))
	sessionID := C.GoString(outSessionID)

	return challenge, sessionID, nil
}

// UpdateSession updates the session with the license payload returned from Apple.
func (c *CDM) UpdateSession(sessionID string, license []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return errors.New("cdm context is closed")
	}

	cSessionID := C.CString(sessionID)
	defer C.free(unsafe.Pointer(cSessionID))

	ret := C.cdm_context_update_session(
		c.ctx,
		cSessionID,
		(*C.uint8_t)(unsafe.Pointer(&license[0])),
		C.uint32_t(len(license)),
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

	if c.ctx == nil {
		return nil, errors.New("cdm context is closed")
	}

	decrypted := make([]byte, len(input))
	var outSize C.uint32_t

	var clearPtr *C.uint16_t
	var cipherPtr *C.uint32_t
	numSubs := C.uint32_t(len(subsamples))
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
		C.uint32_t(len(keyID)),
		(*C.uint8_t)(unsafe.Pointer(&iv[0])),
		C.uint32_t(len(iv)),
		(*C.uint8_t)(unsafe.Pointer(&input[0])),
		C.uint32_t(len(input)),
		clearPtr,
		cipherPtr,
		numSubs,
		(*C.uint8_t)(unsafe.Pointer(&decrypted[0])),
		&outSize,
	)
	if ret != 0 {
		return nil, fmt.Errorf("decryption failed with status %d", ret)
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
