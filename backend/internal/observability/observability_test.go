package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func probe(err error) func(context.Context) error {
	return func(context.Context) error { return err }
}

func TestHealthReady(t *testing.T) {
	t.Parallel()

	down := errors.New("connection refused")
	tests := []struct {
		name       string
		checks     []Check
		wantCode   int
		wantStatus string
		wantDown   string
	}{
		{
			name:       "no checks",
			wantCode:   http.StatusOK,
			wantStatus: "ready",
		},
		{
			name: "all up",
			checks: []Check{
				{Name: "postgres", Critical: true, Probe: probe(nil)},
				{Name: "redis", Probe: probe(nil)},
			},
			wantCode:   http.StatusOK,
			wantStatus: "ready",
		},
		{
			name: "non-critical down stays ready",
			checks: []Check{
				{Name: "postgres", Critical: true, Probe: probe(nil)},
				{Name: "redis", Probe: probe(down)},
			},
			wantCode:   http.StatusOK,
			wantStatus: "ready",
			wantDown:   "redis",
		},
		{
			name: "critical down is not ready",
			checks: []Check{
				{Name: "postgres", Critical: true, Probe: probe(down)},
				{Name: "redis", Probe: probe(nil)},
			},
			wantCode:   http.StatusServiceUnavailable,
			wantStatus: "not_ready",
			wantDown:   "postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			NewHealth(tt.checks...).Ready(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			var body readyResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", body.Status, tt.wantStatus)
			}
			if tt.wantDown != "" {
				res := body.Checks[tt.wantDown]
				if res.Status != "down" || res.Error == "" {
					t.Errorf("check %s = %+v, want down with error", tt.wantDown, res)
				}
			}
		})
	}
}

func TestHealthReadyTimesOut(t *testing.T) {
	t.Parallel()
	slow := Check{Name: "kafka", Critical: true, Probe: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	NewHealth(slow).Ready(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

func TestHealthLive(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	NewHealth(Check{Name: "postgres", Critical: true, Probe: probe(errors.New("down"))}).
		Live(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("liveness must not depend on checks, got %d", rec.Code)
	}
}

func TestNewLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := NewLogger(&buf, "relay", "local", "v1", "warn")
	l.Info("dropped")
	l.Warn("kept")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line at warn level, got %d: %q", len(lines), buf.String())
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for k, want := range map[string]string{"service": "relay", "env": "local", "version": "v1", "msg": "kept"} {
		if rec[k] != want {
			t.Errorf("%s = %v, want %s", k, rec[k], want)
		}
	}
}

func TestOpsServer(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewOpsServer("127.0.0.1:0", NewRegistry(), NewHealth(), logger)

	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()

	for path, want := range map[string]string{"/metrics": "go_goroutines", "/healthz": `"ok"`, "/readyz": `"ready"`} {
		resp, err := http.Get(ts.URL + path) //nolint:noctx // test against local server
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), want) {
			t.Errorf("GET %s = %d %q, want 200 containing %q", path, resp.StatusCode, b, want)
		}
	}
}

func TestOpsServerStartStop(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := NewOpsServer("127.0.0.1:0", NewRegistry(), NewHealth(), logger)

	done := make(chan error, 1)
	go func() { done <- srv.Start(context.Background()) }()
	// Stop may run before Serve; Serve then returns ErrServerClosed, which Start maps to nil.

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("start returned %v after stop", err)
	}
}
