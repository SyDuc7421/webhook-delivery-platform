package secretbox

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func newBox(t *testing.T) *Box {
	t.Helper()
	b, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return b
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("New(%d bytes) must fail", n)
		}
	}
}

func TestSealOpen(t *testing.T) {
	t.Parallel()
	b := newBox(t)
	plain := []byte("whsec_super-secret")
	s1, err := b.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	s2, _ := b.Seal(plain)
	if bytes.Equal(s1, s2) {
		t.Error("two seals of the same plaintext must differ (random nonce)")
	}
	if bytes.Contains(s1, plain) {
		t.Error("ciphertext must not contain the plaintext")
	}
	got, err := b.Open(s1)
	if err != nil || !bytes.Equal(got, plain) {
		t.Errorf("open = %q, %v", got, err)
	}

	tampered := append([]byte(nil), s1...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := b.Open(tampered); err == nil {
		t.Error("tampered ciphertext must fail to open")
	}
	if _, err := b.Open([]byte("short")); err == nil {
		t.Error("too-short input must fail to open")
	}
	other, _ := New(bytes.Repeat([]byte{9}, 32))
	if _, err := other.Open(s1); err == nil {
		t.Error("a different key must fail to open")
	}
}

func TestNewSigningSecret(t *testing.T) {
	t.Parallel()
	s, err := NewSigningSecret()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	raw, found := strings.CutPrefix(s, "whsec_")
	if !found {
		t.Fatalf("secret %q lacks the whsec_ prefix", s)
	}
	dec, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(dec) != 24 {
		t.Errorf("payload must be base64 of 24 bytes: len=%d err=%v", len(dec), err)
	}
	s2, _ := NewSigningSecret()
	if s == s2 {
		t.Error("secrets must differ")
	}
}
