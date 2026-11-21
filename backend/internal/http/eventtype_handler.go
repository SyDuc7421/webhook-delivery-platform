package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

type eventTypeHandler struct {
	svc    *service.CatalogService
	logger *slog.Logger
}

type createEventTypeRequest struct {
	Name           string          `json:"name" validate:"required"`
	Description    string          `json:"description"`
	ExamplePayload json.RawMessage `json:"example_payload"`
}

// updateEventTypeRequest has no name field: names are immutable, so sending one is a 400
// (unknown field).
type updateEventTypeRequest struct {
	Description    *string      `json:"description"`
	ExamplePayload optionalJSON `json:"example_payload"`
}

type eventTypeResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	ExamplePayload json.RawMessage `json:"example_payload"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func toEventType(e domain.EventType) eventTypeResponse {
	example := e.ExamplePayload
	if len(example) == 0 {
		example = json.RawMessage("null")
	}
	return eventTypeResponse{
		ID: e.ID.String(), Name: e.Name, Description: e.Description, ExamplePayload: example,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func (h *eventTypeHandler) create(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	var req createEventTypeRequest
	if err := decodeJSON(w, r, &req, ingestBodyLimit); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	et, err := h.svc.CreateEventType(r.Context(), p.OrgID, service.EventTypeInput{
		Name: req.Name, Description: req.Description, ExamplePayload: rawOrNil(req.ExamplePayload),
	})
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.Header().Set("Location", "/v1/event-types/"+et.ID.String())
	respondJSON(w, http.StatusCreated, toEventType(et))
}

func (h *eventTypeHandler) get(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	et, err := h.svc.GetEventType(r.Context(), p.OrgID, id)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toEventType(et))
}

func (h *eventTypeHandler) list(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	page, err := pageFromQuery(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	rows, next, err := h.svc.ListEventTypes(r.Context(), p.OrgID, page)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	out := make([]eventTypeResponse, 0, len(rows))
	for _, e := range rows {
		out = append(out, toEventType(e))
	}
	respondList(w, out, page.Limit, next)
}

func (h *eventTypeHandler) update(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	var req updateEventTypeRequest
	if derr := decodeJSON(w, r, &req, ingestBodyLimit); derr != nil {
		respondError(w, r, h.logger, derr)
		return
	}
	var example *json.RawMessage
	if req.ExamplePayload.Set {
		v := req.ExamplePayload.Value
		if req.ExamplePayload.isNull() {
			v = nil // null clears the example
		}
		example = &v
	}
	et, err := h.svc.UpdateEventType(r.Context(), p.OrgID, id, req.Description, example)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	respondJSON(w, http.StatusOK, toEventType(et))
}

func (h *eventTypeHandler) delete(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	id, err := idParam(r)
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	if err := h.svc.DeleteEventType(r.Context(), p.OrgID, id); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
