// gen-devtoken generates a short-lived Apple Music developer JWT.
// It reads credentials from environment variables and prints the signed token
// to stdout so CI can capture and inject it at build time.
//
// Required env vars:
//
//	APPLE_KEY_ID      - 10-char key identifier from Apple Developer portal
//	APPLE_TEAM_ID     - 10-char team identifier
//	APPLE_PRIVATE_KEY - PEM-encoded EC private key (.p8 contents)
package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	keyID := mustEnv("APPLE_KEY_ID")
	teamID := mustEnv("APPLE_TEAM_ID")
	privateKeyPEM := mustEnv("APPLE_PRIVATE_KEY")

	ecKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		fatalf("parsing private key: %v", err)
	}

	token := jwt.New(jwt.SigningMethodES256)
	token.Header["kid"] = keyID

	now := time.Now()
	token.Claims = jwt.MapClaims{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(30 * 24 * time.Hour).Unix(),
	}

	signed, err := token.SignedString(ecKey)
	if err != nil {
		fatalf("signing token: %v", err)
	}

	fmt.Print(signed)
}

func parsePrivateKey(pem string) (*ecdsa.PrivateKey, error) {
	block, _ := pemDecode(pem)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing PKCS8: %w", err)
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ECDSA")
	}

	return ecKey, nil
}

// pemDecode is a thin wrapper so tests can inject PEM content without files.
func pemDecode(s string) (*pem.Block, []byte) {
	return pem.Decode([]byte(s))
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fatalf("required environment variable %s is not set", key)
	}
	return v
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-devtoken: "+format+"\n", args...)
	os.Exit(1)
}
