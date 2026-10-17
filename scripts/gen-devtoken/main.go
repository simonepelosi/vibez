// gen-devtoken generates a short-lived Apple Music developer JWT.
// It reads credentials from environment variables and, by default, prints the
// signed token to stdout so CI can capture and inject it at build time.
//
// With -write it instead writes the token into apple_developer_token in the
// vibez config file, which is handy for local (non-embedded) builds.
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
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/simone-vibes/vibez/internal/config"
)

func main() {
	write := flag.Bool("write", false, "write the token into apple_developer_token in the vibez config instead of printing it to stdout")
	configPath := flag.String("config", "", "config file to update with -write (default: ~/.config/vibez/config.json; Windows: %APPDATA%\\vibez\\config.json)")
	flag.Parse()

	keyID := mustEnv("APPLE_KEY_ID")
	teamID := mustEnv("APPLE_TEAM_ID")
	privateKeyPEM := mustEnv("APPLE_PRIVATE_KEY")

	signed, err := generateToken(keyID, teamID, privateKeyPEM)
	if err != nil {
		fatalf("%v", err)
	}

	if !*write {
		fmt.Print(signed)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatalf("loading config: %v", err)
	}
	cfg.AppleDeveloperToken = signed
	if err := cfg.Save(*configPath); err != nil {
		fatalf("saving config: %v", err)
	}
	path, _ := config.ConfigPath(*configPath)
	fmt.Fprintf(os.Stderr, "wrote developer token to %s\n", path)
}

// tokenTTL is how long a generated developer token stays valid.
//
// Apple caps MusicKit developer tokens at six months, 15777000 seconds, and
// rejects anything longer at token-exchange time. Releases embed the token at
// build time, so a short TTL means a binary stops reaching the catalog long
// before it stops being the newest release: the ceiling is the useful value
// here. A rejected token is no longer destructive to the user's session, and
// vibez tells them the build is stale rather than clearing their login.
const tokenTTL = 15777000 * time.Second

// generateToken signs an Apple Music developer JWT valid for tokenTTL.
func generateToken(keyID, teamID, privateKeyPEM string) (string, error) {
	ecKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("parsing private key: %w", err)
	}

	token := jwt.New(jwt.SigningMethodES256)
	token.Header["kid"] = keyID

	now := time.Now()
	token.Claims = jwt.MapClaims{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(tokenTTL).Unix(),
	}

	signed, err := token.SignedString(ecKey)
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}
	return signed, nil
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
