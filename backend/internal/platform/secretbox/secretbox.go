// Package secretbox encrypts endpoint signing secrets at rest with AES-256-GCM. The worker
// needs the plaintext to compute HMAC signatures, so hashing is not an option (FR-E3).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const keySize = 32

// Box seals and opens small secrets. Safe for concurrent use.
type Box struct {
	gcm cipher.AEAD
}

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secretbox key must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Box{gcm: gcm}, nil
}

// Seal returns nonce || ciphertext. A fresh random nonce is used for every call.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	return b.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal. Tampered or foreign input is an error.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	n := b.gcm.NonceSize()
	if len(sealed) < n+b.gcm.Overhead() {
		return nil, errors.New("sealed secret is too short")
	}
	plain, err := b.gcm.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("open sealed secret: %w", err)
	}
	return plain, nil
}

// NewSigningSecret returns a random endpoint signing secret: "whsec_" + base64(24 bytes).
func NewSigningSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random secret: %w", err)
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(b), nil
}
