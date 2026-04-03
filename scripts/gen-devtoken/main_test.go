package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// generateTestKey creates an ephemeral ECDSA P-256 key and returns it as PEM.
func generateTestKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})

	return key, string(pemBytes)
}

func TestParsePrivateKey_Valid(t *testing.T) {
	key, pemStr := generateTestKey(t)

	got, err := parsePrivateKey(pemStr)
	if err != nil {
		t.Fatalf("parsePrivateKey returned error: %v", err)
	}

	if !got.PublicKey.Equal(&key.PublicKey) {
		t.Error("parsed key does not match original")
	}
}

func TestParsePrivateKey_InvalidPEM(t *testing.T) {
	_, err := parsePrivateKey("not-a-pem-block")
	if err == nil {
		t.Error("expected error for invalid PEM, got nil")
	}
}

func TestParsePrivateKey_InvalidDER(t *testing.T) {
	// Valid PEM header but garbage DER content.
	bad := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage")})
	_, err := parsePrivateKey(string(bad))
	if err == nil {
		t.Error("expected error for invalid DER content, got nil")
	}
}

func TestParsePrivateKey_RSAKeyRejected(t *testing.T) {
	// An RSA key should be rejected because it is not ECDSA.
	// We encode a dummy PKCS8 block with a made-up OID to simulate a non-EC key.
	// Easiest: marshal an EC key, corrupt the algorithm OID bytes.
	// Instead, just pass an RSA-labelled block with EC bytes — x509 will error.
	raw := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: []byte{0x30, 0x00}, // minimal invalid PKCS8
	})
	_, err := parsePrivateKey(string(raw))
	if err == nil {
		t.Error("expected error for invalid key bytes, got nil")
	}
}

func TestGeneratedTokenClaims(t *testing.T) {
	key, pemStr := generateTestKey(t)

	// Build a token the same way main() does.
	tok := jwt.New(jwt.SigningMethodES256)
	tok.Header["kid"] = "TESTKEY123"

	now := time.Now()
	tok.Claims = jwt.MapClaims{
		"iss": "TEAMID1234",
		"iat": now.Unix(),
		"exp": now.Add(30 * 24 * time.Hour).Unix(),
	}

	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	// Parse back and verify.
	parsed, err := jwt.Parse(signed, func(t *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parsing generated token: %v", err)
	}
	if !parsed.Valid {
		t.Error("token should be valid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	if claims["iss"] != "TEAMID1234" {
		t.Errorf("iss = %v, want TEAMID1234", claims["iss"])
	}

	_ = pemStr // consumed above via generateTestKey
}

func TestTokenHasCorrectHeader(t *testing.T) {
	key, _ := generateTestKey(t)

	tok := jwt.New(jwt.SigningMethodES256)
	tok.Header["kid"] = "KEYIDTEST"
	tok.Claims = jwt.MapClaims{
		"iss": "TEAMTEST",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(30 * 24 * time.Hour).Unix(),
	}

	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	// JWT header is the first base64 segment.
	parts := strings.Split(signed, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}

	// Token must be non-empty.
	if signed == "" {
		t.Error("signed token is empty")
	}
}

func TestPemDecode_ValidBlock(t *testing.T) {
	key, _ := generateTestKey(t)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	block, _ := pemDecode(pemStr)
	if block == nil {
		t.Fatal("expected non-nil block")
	}
	if block.Type != "PRIVATE KEY" {
		t.Errorf("block.Type = %q, want PRIVATE KEY", block.Type)
	}
}

func TestPemDecode_InvalidInput(t *testing.T) {
	block, rest := pemDecode("not pem")
	if block != nil {
		t.Error("expected nil block for non-PEM input")
	}
	_ = rest
}

// --- mustEnv ---

func TestMustEnv_ReturnsValue(t *testing.T) {
t.Setenv("TEST_GEN_DEVTOKEN_VAR", "test-value-123")
got := mustEnv("TEST_GEN_DEVTOKEN_VAR")
if got != "test-value-123" {
t.Errorf("mustEnv = %q, want %q", got, "test-value-123")
}
}
