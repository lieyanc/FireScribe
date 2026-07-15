// Package auth provides password hashing and session token primitives for
// FireScribe's user accounts. Hashing uses PBKDF2-HMAC-SHA256 from the
// standard library so the single binary keeps building without cgo or extra
// dependencies.
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	hashScheme     = "pbkdf2-sha256"
	hashIterations = 600_000
	hashKeyLength  = 32
	hashSaltLength = 16
)

// HashPassword derives a self-describing hash string:
// pbkdf2-sha256$<iterations>$<salt-b64>$<key-b64>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, hashSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, hashIterations, hashKeyLength)
	if err != nil {
		return "", fmt.Errorf("derive password hash: %w", err)
	}
	return strings.Join([]string{
		hashScheme,
		strconv.Itoa(hashIterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	}, "$"), nil
}

// VerifyPassword reports whether password matches the stored hash. Unknown or
// malformed hashes verify as false rather than erroring, so a corrupt row can
// never authenticate.
func VerifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// FakeVerify burns the same PBKDF2 cost as a real check so login timing does
// not reveal whether a username exists.
func FakeVerify(password string) {
	salt := []byte("firescribe-timing-equalizer-salt")
	_, _ = pbkdf2.Key(sha256.New, password, salt, hashIterations, hashKeyLength)
}

// NewSessionToken returns a fresh random bearer token together with the hash
// under which it is persisted. Only the hash ever touches the database.
func NewSessionToken() (token, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	token = hex.EncodeToString(raw)
	return token, HashSessionToken(token), nil
}

// HashSessionToken maps a bearer token to its storage key.
func HashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
