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

func TestCDM_DecryptBatchValidationAndClosedGuards(t *testing.T) {
	var c CDM
	if _, err := c.DecryptBatch(nil, nil, []BatchSample{{IV: []byte{1}, Input: []byte{1}}}); err == nil {
		t.Fatal("DecryptBatch with nil keyID = nil, want error")
	}
	if _, err := c.DecryptBatch(nil, []byte{1}, []BatchSample{{IV: nil, Input: []byte{1}}}); err == nil {
		t.Fatal("DecryptBatch with empty sample IV = nil, want error")
	}
	if _, err := c.DecryptBatch(nil, []byte{1}, []BatchSample{{
		IV:         []byte{1},
		Input:      []byte{1, 2},
		Subsamples: []Subsample{{ClearBytes: 1, CipherBytes: 5}},
	}}); err == nil {
		t.Fatal("DecryptBatch with subsample sum > input size = nil, want error")
	}
	if out, err := c.DecryptBatch(nil, []byte{1}, nil); err != nil || len(out) != 0 {
		t.Fatalf("DecryptBatch empty samples = (%v, %v), want nil, nil", out, err)
	}
	if _, err := c.DecryptBatch(nil, []byte{1}, []BatchSample{{IV: []byte{1}, Input: []byte{1}}}); err == nil {
		t.Fatal("DecryptBatch on closed CDM = nil, want closed error")
	}
	var nilCDM *CDM
	if _, err := nilCDM.DecryptBatch(nil, []byte{1}, []BatchSample{{IV: []byte{1}, Input: []byte{1}}}); err == nil {
		t.Fatal("(*CDM)(nil).DecryptBatch = nil, want closed error")
	}
}
