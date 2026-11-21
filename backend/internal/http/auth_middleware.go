package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
)

type ctxKey int

const principalKey ctxKey = iota

// Principal is the authenticated dashboard user. OrgID scopes every query (FR-A3).
type Principal struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   domain.Role
}

// PrincipalFrom returns the principal set by requireUser.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// bearerToken extracts the token from "Authorization: Bearer <token>" (scheme case-insensitive).
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// requireUser authenticates a dashboard JWT and stores the Principal in the context.
func requireUser(j *authn.JWT, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				respondError(w, r, logger, fmt.Errorf("missing bearer token: %w", domain.ErrUnauthorized))
				return
			}
			c, err := j.Parse(token)
			if err != nil {
				respondError(w, r, logger, err)
				return
			}
			p := Principal{UserID: c.UserID, OrgID: c.OrgID, Role: domain.Role(c.Role)}
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
		})
	}
}

// requireRole allows only principals with the given role (FR-K1: owners manage API keys).
func requireRole(role domain.Role, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok || p.Role != role {
				respondError(w, r, logger, fmt.Errorf("requires role %s: %w", role, domain.ErrForbidden))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
