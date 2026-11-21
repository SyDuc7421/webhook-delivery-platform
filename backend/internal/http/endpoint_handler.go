package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

type endpointHandler struct {
	svc    *service.EndpointService
	logger *slog.Logger
}

type createEndpointRequest struct {
	URL         string   `json:"url" validate:"required"`
	Description string   `json:"description"`
	EventTypes  []string `json:"event_types"`
	Enabled     *bool    `json:"enabled"`
}

type updateEndpointRequest struct {
	URL         *string   `json:"url"`
	Description *string   `json:"description"`
	EventTypes  *[]string `json:"event_types"`
	Enabled     *bool     `json:"enabled"`
}

type endpointResponse struct {
	ID          string    `json:"id"`
	ConsumerID  string    `json:"consumer_id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	EventTypes  []string  `json:"event_types"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// secretEndpointResponse adds the signing secret. It exists only so the secret can appear in
// create and rotate responses and nowhere else.
type secretEndpointResponse struct {
	endpointResponse
	Secret string `json:"secret"`
}

type rotatedSecretResponse struct {
	Secret string `json:"secret"`
}

func toEndpoint(e domain.Endpoint) endpointResponse {
	subs := e.EventTypes
	if subs == nil {
		subs = []string{}
	}
	return endpointResponse{
		ID: e.ID.String(), ConsumerID: e.ConsumerID.String(), URL: e.URL, Description: e.Description,
		EventTypes: subs, Enabled: e.Enabled, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func (h *endpointHandler) create(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	consumerID, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	var req createEndpointRequest
	if derr := decodeJSON(w, r, &req, smallBody); derr != nil {
		respondError(w, r, h.logger, derr)
		return
	}
	ep, secret, err := h.svc.Create(r.Context(), p.OrgID, consumerID, service.EndpointInput{
		URL: req.URL, Description: req.Description, EventTypes: req.EventTypes, Enabled: req.Enabled,
	})
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", "/v1/endpoints/"+ep.ID.String())
	respondJSON(w, http.StatusCreated, secretEndpointResponse{endpointResponse: toEndpoint(ep), Secret: secret})
}

func (h *endpointHandler) listByConsumer(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	consumerID, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	page, err := pageFromQuery(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	rows, next, err := h.svc.ListByConsumer(r.Context(), p.OrgID, consumerID, page)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	out := make([]endpointResponse, 0, len(rows))
	for _, e := range rows {
		out = append(out, toEndpoint(e))
	}
	respondList(w, out, page.Limit, next)
}

func (h *endpointHandler) get(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	ep, err := h.svc.Get(r.Context(), p.OrgID, id)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toEndpoint(ep))
}

func (h *endpointHandler) update(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	var req updateEndpointRequest
	if derr := decodeJSON(w, r, &req, smallBody); derr != nil {
		respondError(w, r, h.logger, derr)
		return
	}
	ep, err := h.svc.Update(r.Context(), p.OrgID, id, service.EndpointPatch{
		URL: req.URL, Description: req.Description, EventTypes: req.EventTypes, Enabled: req.Enabled,
	})
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toEndpoint(ep))
}

func (h *endpointHandler) rotateSecret(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	secret, err := h.svc.RotateSecret(r.Context(), p.OrgID, id)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, rotatedSecretResponse{Secret: secret})
}

func (h *endpointHandler) delete(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	if err := h.svc.Delete(r.Context(), p.OrgID, id); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
