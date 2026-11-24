package auth

import "testing"

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("supersecret123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !CheckPassword("supersecret123", hash) {
		t.Fatal("expected CheckPassword to return true for valid password")
	}
	if CheckPassword("wrongpass", hash) {
		t.Fatal("expected CheckPassword to return false for invalid password")
	}
}

func TestJWTGenerationAndParsing(t *testing.T) {
	secret := "test-secret"
	token, err := GenerateToken(42, "alice", secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ParseToken(token, secret)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if claims.UserID != 42 || claims.Username != "alice" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	if _, err := ParseToken(token, "wrong-secret"); err == nil {
		t.Fatal("expected ParseToken to fail with wrong secret")
	}
}

