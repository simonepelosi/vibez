//go:build (linux || darwin || windows) && cgo

package cdm

import "testing"

func TestCDM_InputValidationAndClosedGuards(t *testing.T) {
	var c CDM

	if err := c.SetServerCertificate(nil); err == nil {
		t.Fatal("SetServerCertificate(nil) = nil, want error without panicking")
	}
	if err := c.SetServerCertificate([]byte{1}); err == nil {
		t.Fatal("SetServerCertificate on closed CDM = nil, want error")
	}

	if _, _, err := c.GenerateChallenge(nil); err == nil {
		t.Fatal("GenerateChallenge(nil) = nil, want invalid KID length error")
	}
	if _, _, err := c.GenerateChallenge(make([]byte, 16)); err == nil {
		t.Fatal("GenerateChallenge on closed CDM = nil, want closed error")
	}

	if err := c.UpdateSession("", []byte{1}); err == nil {
		t.Fatal("UpdateSession empty sessionID = nil, want error without panicking")
	}
	if err := c.UpdateSession("s1", nil); err == nil {
		t.Fatal("UpdateSession empty license = nil, want error without panicking")
	}
	if err := c.UpdateSession("s1", []byte{1}); err == nil {
		t.Fatal("UpdateSession on closed CDM = nil, want closed error")
	}

	if _, err := c.Decrypt(nil, []byte{1}, []byte{1}); err == nil {
		t.Fatal("Decrypt nil keyID = nil, want error without panicking")
	}
	if _, err := c.Decrypt([]byte{1}, nil, []byte{1}); err == nil {
		t.Fatal("Decrypt nil iv = nil, want error without panicking")
	}
	if out, err := c.Decrypt([]byte{1}, []byte{1}, nil); err != nil || len(out) != 0 {
		t.Fatalf("Decrypt empty ciphertext = (%v, %v), want empty slice and nil error", out, err)
	}
	if _, err := c.Decrypt([]byte{1}, []byte{1}, []byte{1}); err == nil {
		t.Fatal("Decrypt on closed CDM = nil, want closed error")
	}

	if err := c.CloseSession(""); err != nil {
		t.Fatalf("CloseSession empty sessionID = %v, want nil", err)
	}
	if err := c.CloseSession("s1"); err == nil {
		t.Fatal("CloseSession on closed CDM = nil, want closed error")
	}
	var nilCDM *CDM
	if err := nilCDM.CloseSession("s1"); err != nil {
		t.Fatalf("(*CDM)(nil).CloseSession = %v, want nil", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() on uninitialized CDM = %v, want nil", err)
	}
}
