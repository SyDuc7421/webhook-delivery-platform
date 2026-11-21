package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
)

type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }

func TestRequireUser(t *testing.T) {
	t.Parallel()
	clock := &stepClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	j := authn.NewJWT([]byte("middleware-test-secret-32-bytes-long"), 15*time.Minute, clock)
	userID, orgID := uuid.New(), uuid.New()
	valid, _, err := j.Issue(authn.Claims{UserID: userID, OrgID: orgID, Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	expiredIssuer := authn.NewJWT([]byte("middleware-test-secret-32-bytes-long"), 15*time.Minute,
		&stepClock{now: clock.now.Add(-time.Hour)})
	expired, _, err := expiredIssuer.Issue(authn.Claims{UserID: userID, OrgID: orgID, Role: "owner"})
	if err != nil {
		t.Fatal(err)
	}

	var seen Principal
	h := requireUser(j, discard)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFrom(r.Context())
		if !ok {
			t.Error("principal missing in handler")
		}
		seen = p
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"basic scheme", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
		{"expired", "Bearer " + expired, http.StatusUnauthorized},
		{"garbage", "Bearer abc", http.StatusUnauthorized},
		{"valid", "Bearer " + valid, http.StatusNoContent},
		{"valid lowercase scheme", "bearer " + valid, http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("code = %d, want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized {
				if b := decodeError(t, rec.Body); b.Error.Code != "UNAUTHORIZED" {
					t.Errorf("code = %q", b.Error.Code)
				}
			}
		})
	}
	if seen.UserID != userID || seen.OrgID != orgID || seen.Role != domain.RoleOwner {
		t.Errorf("principal = %+v", seen)
	}
}

func TestRequireRole(t *testing.T) {
	t.Parallel()
	h := requireRole(domain.RoleOwner, discard)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for role, want := range map[domain.Role]int{domain.RoleOwner: http.StatusNoContent, domain.RoleMember: http.StatusForbidden} {
		req := httptest.NewRequestWithContext(withPrincipal(t.Context(), Principal{Role: role}), http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("role %s: code = %d, want %d", role, rec.Code, want)
		}
	}
}
