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

const apiKeyPrefix = "whk_"

// NewAPIKey returns a random API key ("whk_" + base64url of 32 bytes), its visible prefix and
// the sha256 hash that is stored instead of the key (FR-K2).
func NewAPIKey() (full, prefix string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", nil, fmt.Errorf("read random api key: %w", err)
	}
	full = apiKeyPrefix + base64.RawURLEncoding.EncodeToString(b)
	return full, full[:12], HashAPIKey(full), nil
}

// HashAPIKey returns sha256(full key), the lookup value for authentication.
func HashAPIKey(full string) []byte {
	sum := sha256.Sum256([]byte(full))
	return sum[:]
}

// LooksLikeAPIKey is a cheap shape check so garbage never costs a database lookup.
func LooksLikeAPIKey(s string) bool {
	return len(s) == 47 && s[:4] == apiKeyPrefix
}
