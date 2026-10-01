//go:build !cgo || (!linux && !darwin)

package cdm

import (
	"errors"
)

var errNoCgo = errors.New("browserless CDM requires CGo")

type CDM struct{}

func New(libraryPath string) (*CDM, error) {
	return nil, errNoCgo
}

func (c *CDM) SetServerCertificate(cert []byte) error {
	return errNoCgo
}

func (c *CDM) GenerateChallenge(kid []byte) ([]byte, string, error) {
	return nil, "", errNoCgo
}

func (c *CDM) UpdateSession(sessionID string, license []byte) error {
	return errNoCgo
}

func (c *CDM) Decrypt(keyID, iv, ciphertext []byte) ([]byte, error) {
	return nil, errNoCgo
}

func (c *CDM) Close() error {
	return nil
}
