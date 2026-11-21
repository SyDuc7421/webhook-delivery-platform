package authn

import (
	"encoding/base64"
	"testing"
)

func TestOpaqueToken(t *testing.T) {
	t.Parallel()
	tok1, hash1, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	tok2, _, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if tok1 == tok2 {
		t.Error("tokens must differ")
	}
	if HashOpaqueToken(tok1) != hash1 {
		t.Error("HashOpaqueToken must reproduce the returned hash")
	}
	if len(hash1) != 64 {
		t.Errorf("hash len = %d, want 64 hex chars (sha256)", len(hash1))
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok1)
	if err != nil || len(raw) != 32 {
		t.Errorf("token must be base64url of 32 bytes: len=%d err=%v", len(raw), err)
	}
}
