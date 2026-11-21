package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type fakeLimiter struct {
	allowed    bool
	retryAfter time.Duration
	err        error
	calls      int
	lastOrg    uuid.UUID
}

func (f *fakeLimiter) Allow(_ context.Context, orgID uuid.UUID) (bool, time.Duration, error) {
	f.calls++
	f.lastOrg = orgID
	return f.allowed, f.retryAfter, f.err
}

func serveRateLimited(t *testing.T, rl domain.RateLimiter, org uuid.UUID, withOrg bool) (*httptest.ResponseRecorder, prometheus.Counter, *bool) {
	t.Helper()
	failOpen := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_fail_open_total"})
	called := false
	h := rateLimit(rl, discard, failOpen)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	ctx := t.Context()
	if withOrg {
		ctx = withOrgID(ctx, org)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/events", nil))
	return rec, failOpen, &called
}

func TestRateLimitAllows(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	rl := &fakeLimiter{allowed: true}
	rec, failOpen, called := serveRateLimited(t, rl, org, true)
	if rec.Code != http.StatusAccepted || !*called {
		t.Errorf("code = %d, handler called = %v", rec.Code, *called)
	}
	if rl.lastOrg != org {
		t.Errorf("limiter saw org %v, want %v", rl.lastOrg, org)
	}
	if testutil.ToFloat64(failOpen) != 0 {
		t.Error("fail-open counter must stay 0")
	}
}

func TestRateLimitDenies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		retryAfter time.Duration
		want       string
	}{
		{1500 * time.Millisecond, "2"},
		{time.Second, "1"},
		{200 * time.Millisecond, "1"},
		{0, "1"},
		{59*time.Second + time.Millisecond, "60"},
	}
	for _, tt := range tests {
		rec, _, called := serveRateLimited(t, &fakeLimiter{allowed: false, retryAfter: tt.retryAfter}, uuid.New(), true)
		if rec.Code != http.StatusTooManyRequests || *called {
			t.Fatalf("code = %d, handler called = %v", rec.Code, *called)
		}
		if got := rec.Header().Get("Retry-After"); got != tt.want {
			t.Errorf("retryAfter %v: Retry-After = %q, want %q", tt.retryAfter, got, tt.want)
		}
		if b := decodeError(t, rec.Body); b.Error.Code != "RATE_LIMITED" {
			t.Errorf("code = %q", b.Error.Code)
		}
	}
}

func TestRateLimitFailsOpen(t *testing.T) {
	t.Parallel()
	rec, failOpen, called := serveRateLimited(t, &fakeLimiter{err: errors.New("redis: connection refused")}, uuid.New(), true)
	if rec.Code != http.StatusAccepted || !*called {
		t.Errorf("code = %d, handler called = %v; a limiter error must let the request through (NFR-D1)", rec.Code, *called)
	}
	if got := testutil.ToFloat64(failOpen); got != 1 {
		t.Errorf("fail-open counter = %v, want 1", got)
	}
}

func TestRateLimitWithoutOrgIsUnauthorized(t *testing.T) {
	t.Parallel()
	rl := &fakeLimiter{allowed: true}
	rec, _, called := serveRateLimited(t, rl, uuid.Nil, false)
	if rec.Code != http.StatusUnauthorized || *called || rl.calls != 0 {
		t.Errorf("code = %d, handler called = %v, limiter calls = %d", rec.Code, *called, rl.calls)
	}
}
