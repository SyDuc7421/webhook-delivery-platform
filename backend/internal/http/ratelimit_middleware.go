package http

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type orgKey struct{}

// withOrgID stores the org authenticated by an API key.
func withOrgID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, orgKey{}, id)
}

// orgIDFrom returns the org set by requireAPIKey.
func orgIDFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(orgKey{}).(uuid.UUID)
	return id, ok
}

// rateLimit limits requests per org (FR-I5) and counts denials in denied. When the limiter is
// unavailable the request is let through and counted in failOpen, so ingest never depends on
// Redis (NFR-D1).
func rateLimit(rl domain.RateLimiter, logger *slog.Logger, failOpen, denied prometheus.Counter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID, ok := orgIDFrom(r.Context())
			if !ok {
				respondError(w, r, logger, fmt.Errorf("rate limit without an authenticated org: %w", domain.ErrUnauthorized))
				return
			}
			allowed, retryAfter, err := rl.Allow(r.Context(), orgID)
			if err != nil {
				failOpen.Inc()
				logger.WarnContext(r.Context(), "rate limit unavailable, failing open", slog.String("error", err.Error()))
				next.ServeHTTP(w, r)
				return
			}
			if !allowed {
				denied.Inc()
				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(retryAfter)))
				respondError(w, r, logger, domain.ErrRateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// retryAfterSeconds rounds up and never returns less than 1.
func retryAfterSeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}
