package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("正确的密码-secret1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !VerifyPassword(hash, "正确的密码-secret1") {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("wrong password verified")
	}

	second, err := HashPassword("正确的密码-secret1")
	if err != nil {
		t.Fatal(err)
	}
	if second == hash {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, stored := range []string{
		"",
		"plaintext",
		"bcrypt$10$abc$def",
		"pbkdf2-sha256$notanumber$c2FsdA$aGFzaA",
		"pbkdf2-sha256$1000$*bad*$aGFzaA",
		"pbkdf2-sha256$1000$c2FsdA$",
	} {
		if VerifyPassword(stored, "anything") {
			t.Fatalf("malformed hash %q verified", stored)
		}
	}
}

func TestSessionTokens(t *testing.T) {
	token, tokenHash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(token))
	}
	if HashSessionToken(token) != tokenHash {
		t.Fatal("token hash mismatch")
	}
	other, otherHash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if other == token || otherHash == tokenHash {
		t.Fatal("session tokens must be unique")
	}
}
