package authn

import (
	"strings"
	"testing"
)

func TestHashVerifyPassword(t *testing.T) {
	t.Parallel()
	h1, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(h1, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Errorf("encoded = %q, want argon2id PHC string with m=65536,t=3,p=2", h1)
	}
	h2, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password must differ (random salt)")
	}

	tests := []struct {
		name    string
		encoded string
		pw      string
		want    bool
		wantErr bool
	}{
		{"correct", h1, "correct horse battery staple", true, false},
		{"wrong", h1, "Correct horse battery staple", false, false},
		{"malformed", "$argon2id$v=19$nonsense", "x", false, true},
		{"not argon2", "$2a$10$abcdefghijklmnopqrstuv", "x", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := VerifyPassword(tt.encoded, tt.pw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("VerifyPassword = %v, want %v", got, tt.want)
			}
		})
	}
}
