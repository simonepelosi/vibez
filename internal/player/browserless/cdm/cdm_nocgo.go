//go:build !cgo || (!linux && !darwin)

package cdm

import (
	"errors"
)

var errNoCgo = errors.New("browserless CDM requires CGo")

type CDM struct{}

type Subsample struct {
	ClearBytes  uint16
	CipherBytes uint32
}

func New(_ string) (*CDM, error) {
	return nil, errNoCgo
}

func (c *CDM) SetServerCertificate(_ []byte) error {
	return errNoCgo
}

func (c *CDM) GenerateChallenge(_ []byte) ([]byte, string, error) {
	return nil, "", errNoCgo
}

func (c *CDM) UpdateSession(_ string, _ []byte) error {
	return errNoCgo
}

func (c *CDM) Decrypt(_, _, _ []byte) ([]byte, error) {
	return nil, errNoCgo
}

func (c *CDM) DecryptSubsamples(_, _, _ []byte, _ []Subsample) ([]byte, error) {
	return nil, errNoCgo
}

func (c *CDM) Close() error {
	return nil
}
