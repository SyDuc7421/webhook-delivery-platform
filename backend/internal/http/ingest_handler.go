package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
)

// ingestEnvelopeSlack is room for the JSON around the payload (consumer_uid, type, keys).
const ingestEnvelopeSlack = 16 << 10

// traceparentRE is the W3C trace-context shape: version-traceid-spanid-flags, lowercase hex.
var traceparentRE = regexp.MustCompile(`^[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)

// validTraceparent reports whether v is a usable W3C traceparent. An invalid one is ignored,
// never stored: it comes from the client, so it must not bypass the payload cap or break the insert.
func validTraceparent(v string) bool {
	return len(v) == 55 && traceparentRE.MatchString(v) &&
		v[:2] != "ff" && v[3:35] != "00000000000000000000000000000000" && v[36:52] != "0000000000000000"
}

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
	traceparent := r.Header.Get("traceparent")
	if !validTraceparent(traceparent) {
		traceparent = ""
	}
	res, err := h.svc.Ingest(r.Context(), service.IngestInput{
		OrgID: orgID, ConsumerUID: req.ConsumerUID, Type: req.Type, Payload: req.Payload,
		IdempotencyKey: r.Header.Get("Idempotency-Key"), Traceparent: traceparent,
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
