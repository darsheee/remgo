package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashing(t *testing.T) {
	pass := "supersecret123"
	hash, err := HashPassword(pass)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == pass {
		t.Fatalf("hash should not equal password")
	}

	if !CheckPassword(hash, pass) {
		t.Errorf("CheckPassword failed with correct password")
	}
	if CheckPassword(hash, "wrongpassword") {
		t.Errorf("CheckPassword succeeded with incorrect password")
	}
}

func TestPATGenerationAndHashing(t *testing.T) {
	pat, err := GeneratePAT()
	if err != nil {
		t.Fatalf("GeneratePAT failed: %v", err)
	}
	if !strings.HasPrefix(pat, "remgo_pat_") {
		t.Errorf("expected remgo_pat_ prefix, got %s", pat)
	}
	if len(pat) < 30 {
		t.Errorf("PAT too short: %s", pat)
	}

	hash1 := HashToken(pat)
	hash2 := HashToken(pat)
	if hash1 != hash2 {
		t.Errorf("HashToken not deterministic")
	}
	if len(hash1) != 64 { // SHA-256 hex is 64 characters
		t.Errorf("expected 64 hex chars, got %d", len(hash1))
	}
}

func TestJWTCreateAndValidate(t *testing.T) {
	secret := []byte("remgo-secret-key-32-bytes-long!")
	claims := JWTClaims{
		UserID:    "usr_123",
		Username:  "alice",
		Email:     "alice@example.com",
		Role:      "admin",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	token, err := CreateJWT(secret, claims)
	if err != nil {
		t.Fatalf("CreateJWT failed: %v", err)
	}

	validated, err := ValidateJWT(secret, token)
	if err != nil {
		t.Fatalf("ValidateJWT failed: %v", err)
	}
	if validated.UserID != "usr_123" || validated.Username != "alice" || validated.Role != "admin" {
		t.Errorf("claims mismatch: %+v", validated)
	}

	// Test invalid signature with different secret
	wrongSecret := []byte("wrong-secret-key-32-bytes-long!")
	_, err = ValidateJWT(wrongSecret, token)
	if err == nil || err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}

	// Test expired token
	expiredClaims := JWTClaims{
		UserID:    "usr_expired",
		Username:  "bob",
		IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	}
	expiredToken, _ := CreateJWT(secret, expiredClaims)
	_, err = ValidateJWT(secret, expiredToken)
	if err == nil || err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}
