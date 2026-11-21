package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

type apiKeyHandler struct {
	svc    *service.APIKeyService
	logger *slog.Logger
}

type createAPIKeyRequest struct {
	Name string `json:"name" validate:"required,max=100"`
}

type apiKeyResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}

type createdAPIKeyResponse struct {
	apiKeyResponse
	Key string `json:"key"` // shown exactly once (FR-K1)
}

func toAPIKey(k domain.APIKey) apiKeyResponse {
	return apiKeyResponse{ID: k.ID.String(), Name: k.Name, Prefix: k.Prefix, CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt}
}

func (h *apiKeyHandler) create(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req createAPIKeyRequest
	if err := decodeJSON(w, r, &req, smallBody); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	k, full, err := h.svc.Create(r.Context(), p.OrgID, p.UserID, req.Name)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", "/v1/api-keys/"+k.ID.String())
	respondJSON(w, http.StatusCreated, createdAPIKeyResponse{apiKeyResponse: toAPIKey(k), Key: full})
}

func (h *apiKeyHandler) list(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	page, err := pageFromQuery(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	keys, next, err := h.svc.List(r.Context(), p.OrgID, page)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	out := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAPIKey(k))
	}
	respondList(w, out, page.Limit, next)
}

func (h *apiKeyHandler) revoke(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, h.logger, domain.ErrNotFound) // a malformed id cannot exist
		return
	}
	if err := h.svc.Revoke(r.Context(), p.OrgID, id); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
