package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type signupReq struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12,max=128"`
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		max        int64
		wantErr    error
		wantFields map[string]string
	}{
		{name: "valid", body: `{"email":"a@b.co","password":"0123456789ab"}`, max: 1024},
		{
			name: "unknown field", body: `{"email":"a@b.co","password":"0123456789ab","admin":true}`, max: 1024,
			wantErr: domain.ErrInvalidInput, wantFields: map[string]string{"body": "unknown field \"admin\""},
		},
		{
			name: "trailing data", body: `{"email":"a@b.co","password":"0123456789ab"}{}`, max: 1024,
			wantErr: domain.ErrInvalidInput, wantFields: map[string]string{"body": "must contain a single JSON value"},
		},
		{
			name: "malformed", body: `{"email":`, max: 1024,
			wantErr: domain.ErrInvalidInput, wantFields: map[string]string{"body": "malformed JSON"},
		},
		{
			name: "empty", body: ``, max: 1024,
			wantErr: domain.ErrInvalidInput, wantFields: map[string]string{"body": "is required"},
		},
		{name: "too large", body: `{"email":"` + strings.Repeat("a", 2000) + `"}`, max: 1024, wantErr: domain.ErrPayloadTooBig},
		{
			name: "validation", body: `{"email":"x","password":"short"}`, max: 1024, wantErr: domain.ErrInvalidInput,
			wantFields: map[string]string{"email": "must be a valid email", "password": "must be at least 12 characters"},
		},
		{
			name: "required", body: `{}`, max: 1024, wantErr: domain.ErrInvalidInput,
			wantFields: map[string]string{"email": "is required", "password": "is required"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			var dst signupReq
			err := decodeJSON(httptest.NewRecorder(), r, &dst, tt.max)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantFields == nil {
				return
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err %T is not *ValidationError", err)
			}
			got := map[string]string{}
			for _, f := range ve.Fields {
				got[f.Field] = f.Issue
			}
			for field, issue := range tt.wantFields {
				if got[field] != issue {
					t.Errorf("field %s issue = %q, want %q (all: %v)", field, got[field], issue, got)
				}
			}
		})
	}
}

func TestRespondErrorIncludesDetails(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	err := &domain.ValidationError{Fields: []domain.FieldError{{Field: "url", Issue: "must be https"}}}
	respondError(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil), discard, err)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
	b := decodeError(t, rec.Body)
	if b.Error.Code != "VALIDATION_FAILED" || len(b.Error.Details) != 1 ||
		b.Error.Details[0].Field != "url" || b.Error.Details[0].Issue != "must be https" {
		t.Errorf("body = %+v", b)
	}
}

func TestUnprocessableMapsTo422(t *testing.T) {
	t.Parallel()
	m := mapError(&domain.ValidationError{Cause: domain.ErrUnprocessable, Fields: []domain.FieldError{{Field: "event_types", Issue: "unknown: x"}}})
	if m.status != http.StatusUnprocessableEntity || m.code != "UNPROCESSABLE_ENTITY" {
		t.Errorf("mapped = %+v", m)
	}
}
