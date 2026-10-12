package auth

import (
	"bytes"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("invalid password accepted")
	}
}

func TestTokens(t *testing.T) {
	tok, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 {
		t.Fatalf("token length = %d, want 43 (256 bits base64url)", len(tok))
	}
	if !bytes.Equal(HashToken(tok), hash) {
		t.Fatal("hash mismatch")
	}
	tok2, _, _ := NewToken()
	if tok == tok2 {
		t.Fatal("tokens must be random")
	}
}
