package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// ingestEnvelopeSlack is room for the JSON around the payload (consumer_uid, type, keys).
const ingestEnvelopeSlack = 16 << 10

type ingestHandler struct {
	svc        *service.IngestService
	maxPayload int
	logger     *slog.Logger
}

type ingestRequest struct {
	ConsumerUID string          `json:"consumer_uid" validate:"required"`
	Type        string          `json:"type" validate:"required"`
	Payload     json.RawMessage `json:"payload" validate:"required"`
}

type ingestResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// handle serves POST /v1/events (FR-I1). It answers 202 as soon as the event and its outbox
// row are committed; Kafka is not involved in the request (FR-I4).
func (h *ingestHandler) handle(w http.ResponseWriter, r *http.Request) {
	orgID, _ := orgIDFrom(r.Context()) // requireAPIKey guarantees it
	var req ingestRequest
	if err := decodeJSON(w, r, &req, int64(h.maxPayload+ingestEnvelopeSlack)); err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	res, err := h.svc.Ingest(r.Context(), service.IngestInput{
		OrgID: orgID, ConsumerUID: req.ConsumerUID, Type: req.Type, Payload: req.Payload,
		IdempotencyKey: r.Header.Get("Idempotency-Key"), Traceparent: r.Header.Get("traceparent"),
	})
	if err != nil {
		respondError(w, r, h.logger, err)
		return
	}
	if res.Duplicate {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	respondJSON(w, http.StatusAccepted, ingestResponse{ID: res.EventID.String(), Status: "accepted"})
}
