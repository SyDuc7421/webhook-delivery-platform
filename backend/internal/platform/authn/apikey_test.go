package authn

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestNewAPIKey(t *testing.T) {
	t.Parallel()
	full, prefix, hash, err := NewAPIKey()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !strings.HasPrefix(full, "whk_") || len(full) != 47 {
		t.Errorf("key = %q (len %d), want whk_ + 43 chars", full, len(full))
	}
	if prefix != full[:12] {
		t.Errorf("prefix = %q, want first 12 chars of the key", prefix)
	}
	if want := sha256.Sum256([]byte(full)); string(hash) != string(want[:]) {
		t.Error("hash must be sha256 of the full key")
	}
	if string(HashAPIKey(full)) != string(hash) {
		t.Error("HashAPIKey must reproduce the stored hash")
	}
	other, _, _, _ := NewAPIKey()
	if other == full {
		t.Error("keys must differ")
	}
}
