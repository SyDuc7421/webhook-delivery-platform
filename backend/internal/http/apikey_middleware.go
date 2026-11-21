package http

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// requireAPIKey authenticates the ingest API with "Authorization: Bearer whk_..." and stores
// the key's org in the context. Revoked keys are rejected immediately (FR-K3).
func requireAPIKey(auth *service.APIKeyService, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				respondError(w, r, logger, fmt.Errorf("missing api key: %w", domain.ErrUnauthorized))
				return
			}
			orgID, err := auth.Authenticate(r.Context(), token)
			if err != nil {
				respondError(w, r, logger, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(withOrgID(r.Context(), orgID)))
		})
	}
}
