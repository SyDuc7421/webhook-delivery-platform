package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	in := domain.Cursor{CreatedAt: time.Date(2026, 11, 21, 10, 30, 15, 123456000, time.UTC), ID: uuid.New()}
	enc := EncodeCursor(in)
	if enc == "" {
		t.Fatal("empty cursor")
	}
	out, err := DecodeCursor(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.CreatedAt.Equal(in.CreatedAt) || out.ID != in.ID {
		t.Errorf("round trip = %+v, want %+v (microsecond precision must survive)", out, in)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"not base64 !!", "e30", "bm90LWpzb24", "eyJ0IjoiMjAyNi0xMS0yMVQxMDowMDowMFoiLCJpIjoibm90LWEtdXVpZCJ9"} {
		_, err := DecodeCursor(s)
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Fields[0].Field != "cursor" {
			t.Errorf("DecodeCursor(%q) err = %v, want ValidationError on field cursor", s, err)
		}
	}
}

func TestParseLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 20, false},
		{"1", 1, false},
		{"100", 100, false},
		{"0", 0, true},
		{"101", 0, true},
		{"-5", 0, true},
		{"x", 0, true},
		{"10.5", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseLimit(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseLimit(%q) = %d, %v; want %d, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
		if tt.wantErr {
			var ve *domain.ValidationError
			if !errors.As(err, &ve) || ve.Fields[0].Field != "limit" {
				t.Errorf("ParseLimit(%q) err = %v, want ValidationError on field limit", tt.in, err)
			}
		}
	}
}
