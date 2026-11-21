// Package authn holds credential primitives: argon2id passwords, JWT access tokens and
// opaque random tokens. It has no storage and no business rules.
package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (OWASP baseline: m=64 MiB, t=3, p=2).
const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	saltLen      = 16
	keyLen       = 32
)

var errMalformedHash = errors.New("malformed password hash")

// HashPassword returns a PHC-formatted argon2id hash with a random salt.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword reports whether pw matches the encoded hash, comparing in constant time.
// The hash's own parameters are used, so stored hashes survive a parameter change.
func VerifyPassword(encoded, pw string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedHash
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, errMalformedHash
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, errMalformedHash
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errMalformedHash
	}
	got := argon2.IDKey([]byte(pw), salt, iterations, memory, threads, uint32(len(want))) //nolint:gosec // len of a 32-byte key
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
