package auth

import (
	"testing"
	"time"

	"github.com/neonphnx/NeonServices/internal/database"
)

func TestPasswordHashing(t *testing.T) {
	password := "superSecret123!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !CheckPassword(password, hash) {
		t.Errorf("CheckPassword returned false for matching password")
	}

	if CheckPassword("wrongPassword", hash) {
		t.Errorf("CheckPassword returned true for wrong password")
	}
}

func TestJWTGenerationAndValidation(t *testing.T) {
	secret := "test-secret-key-32-bytes-long-min"
	token, err := GenerateToken(42, "testuser", database.RoleUser, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != 42 || claims.Username != "testuser" || claims.Role != database.RoleUser {
		t.Errorf("Claims mismatch: got %+v", claims)
	}

	// Validate with wrong secret
	_, err = ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Errorf("ValidateToken should fail with wrong secret")
	}
}
