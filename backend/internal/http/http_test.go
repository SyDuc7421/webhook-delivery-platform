package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/observability"
)

var discard = slog.New(slog.NewJSONHandler(io.Discard, nil))

func TestMapError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err      error
		wantCode int
		wantStr  string
	}{
		{fmt.Errorf("get endpoint 1: %w", domain.ErrNotFound), http.StatusNotFound, "RESOURCE_NOT_FOUND"},
		{domain.ErrInvalidInput, http.StatusBadRequest, "VALIDATION_FAILED"},
		{domain.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
		{domain.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{domain.ErrAlreadyExists, http.StatusConflict, "ALREADY_EXISTS"},
		{domain.ErrConflict, http.StatusConflict, "CONFLICT"},
		{domain.ErrPayloadTooBig, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
		{domain.ErrBlockedHost, http.StatusUnprocessableEntity, "BLOCKED_HOST"},
		{domain.ErrRateLimited, http.StatusTooManyRequests, "RATE_LIMITED"},
		{domain.ErrUnavailable, http.StatusServiceUnavailable, "UNAVAILABLE"},
		{errors.New("pq: something exploded"), http.StatusInternalServerError, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.wantStr, func(t *testing.T) {
			t.Parallel()
			m := mapError(tt.err)
			if m.status != tt.wantCode || m.code != tt.wantStr {
				t.Errorf("mapError(%v) = %d %s, want %d %s", tt.err, m.status, m.code, tt.wantCode, tt.wantStr)
			}
		})
	}
}

func decodeError(t *testing.T, body io.Reader) errorBody {
	t.Helper()
	var b errorBody
	if err := json.NewDecoder(body).Decode(&b); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return b
}

func TestRouter(t *testing.T) {
	t.Parallel()
	h := NewRouter(RouterDeps{Logger: discard, Health: observability.NewHealth()})

	tests := []struct {
		name     string
		method   string
		path     string
		reqID    string
		wantCode int
		wantErr  string
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", wantCode: http.StatusOK},
		{name: "readyz", method: http.MethodGet, path: "/readyz", wantCode: http.StatusOK},
		{name: "unknown route", method: http.MethodGet, path: "/v1/nope", wantCode: http.StatusNotFound, wantErr: "RESOURCE_NOT_FOUND"},
		{name: "wrong method", method: http.MethodPost, path: "/healthz", wantCode: http.StatusMethodNotAllowed, wantErr: "METHOD_NOT_ALLOWED"},
		{name: "request id propagated", method: http.MethodGet, path: "/v1/nope", reqID: "req-123", wantCode: http.StatusNotFound, wantErr: "RESOURCE_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			if tt.reqID != "" {
				req.Header.Set("X-Request-Id", tt.reqID)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			gotID := rec.Header().Get("X-Request-Id")
			if gotID == "" || (tt.reqID != "" && gotID != tt.reqID) {
				t.Errorf("X-Request-Id = %q, want %q (or generated)", gotID, tt.reqID)
			}
			if tt.wantErr != "" {
				b := decodeError(t, rec.Body)
				if b.Error.Code != tt.wantErr {
					t.Errorf("error code = %q, want %q", b.Error.Code, tt.wantErr)
				}
				if tt.wantCode == http.StatusNotFound && b.RequestID != gotID {
					t.Errorf("body request_id = %q, header = %q", b.RequestID, gotID)
				}
			}
		})
	}
}

func TestRecovererReturnsEnvelope(t *testing.T) {
	t.Parallel()
	h := recoverer(discard)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom") //nolint:forbidigo // exercising the recoverer
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
	if b := decodeError(t, rec.Body); b.Error.Code != "INTERNAL" {
		t.Errorf("code = %q", b.Error.Code)
	}
}

func TestRespondErrorDoesNotLeakInternalMessage(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	respondError(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), discard, errors.New("password=hunter2"))
	b := decodeError(t, rec.Body)
	if b.Error.Message != "unexpected error" {
		t.Errorf("message = %q, internal error text must not reach the client", b.Error.Message)
	}
}

func TestRespondJSON(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	respondJSON(rec, http.StatusCreated, map[string]string{"id": "e1"})
	if rec.Code != http.StatusCreated || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("code=%d content-type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	var b struct {
		Data map[string]string `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&b); err != nil || b.Data["id"] != "e1" {
		t.Errorf("body = %+v, err = %v", b, err)
	}
}

func TestServerStartStop(t *testing.T) {
	t.Parallel()
	s := NewServer(0, time.Second, NewRouter(RouterDeps{Logger: discard, Health: observability.NewHealth()}), discard)
	s.srv.Addr = "127.0.0.1:0"
	done := make(chan error, 1)
	go func() { done <- s.Start(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("start after stop = %v", err)
	}
	if s.Name() != "http-server" {
		t.Errorf("name = %q", s.Name())
	}
}
