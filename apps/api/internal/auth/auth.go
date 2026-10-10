// Package auth provides password hashing and opaque session tokens.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost balances login latency against offline cracking cost.
const BcryptCost = 12

// ErrInvalidCredentials is returned for any failed login, without revealing
// whether the email exists.
var ErrInvalidCredentials = errors.New("invalid email or password")

// HashPassword returns a bcrypt hash.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	return string(b), err
}

// CheckPassword compares a password with a bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash lets login spend the same bcrypt time for unknown emails, so
// response timing does not reveal which addresses are registered.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("pulseops-timing-equaliser"), BcryptCost)

// EqualiseTiming performs a throwaway bcrypt comparison.
func EqualiseTiming(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

// NewToken returns a random 256-bit bearer token and its SHA-256 hash. Only
// the hash is persisted, so a database leak does not leak live sessions.
func NewToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken hashes a presented bearer token for lookup.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
