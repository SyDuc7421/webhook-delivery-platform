package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// NewOpaqueToken returns a random 32-byte token (base64url) and its sha256 hex hash.
// Only the hash is stored, so a leaked store does not leak usable tokens.
func NewOpaqueToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("read random token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashOpaqueToken(token), nil
}

// HashOpaqueToken returns the storage key for an opaque token.
func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
