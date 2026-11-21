package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

type consumerHandler struct {
	svc    *service.CatalogService
	logger *slog.Logger
}

type createConsumerRequest struct {
	UID  string `json:"uid" validate:"required"`
	Name string `json:"name"`
}

// updateConsumerRequest has no uid: it is immutable, so sending one is a 400 (unknown field).
type updateConsumerRequest struct {
	Name string `json:"name" validate:"required"`
}

type consumerResponse struct {
	ID        string    `json:"id"`
	UID       string    `json:"uid"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toConsumer(c domain.Consumer) consumerResponse {
	return consumerResponse{ID: c.ID.String(), UID: c.UID, Name: c.Name, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func (h *consumerHandler) create(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req createConsumerRequest
	if err := decodeJSON(w, r, &req, smallBody); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	c, err := h.svc.CreateConsumer(r.Context(), p.OrgID, req.UID, req.Name)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", "/v1/consumers/"+c.ID.String())
	respondJSON(w, http.StatusCreated, toConsumer(c))
}

func (h *consumerHandler) get(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	c, err := h.svc.GetConsumer(r.Context(), p.OrgID, id)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toConsumer(c))
}

func (h *consumerHandler) list(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	page, err := pageFromQuery(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	rows, next, err := h.svc.ListConsumers(r.Context(), p.OrgID, page)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	out := make([]consumerResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, toConsumer(c))
	}
	respondList(w, out, page.Limit, next)
}

func (h *consumerHandler) update(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	var req updateConsumerRequest
	if derr := decodeJSON(w, r, &req, smallBody); derr != nil {
		respondError(w, r, h.logger, derr)
		return
	}
	c, err := h.svc.UpdateConsumer(r.Context(), p.OrgID, id, req.Name)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toConsumer(c))
}

func (h *consumerHandler) delete(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	if err := h.svc.DeleteConsumer(r.Context(), p.OrgID, id); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
