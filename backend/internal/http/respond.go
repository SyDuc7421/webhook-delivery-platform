package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// errorBody is the only error shape the API returns (docs/API.md §Response envelope).
type errorBody struct {
	Error     errorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

type errorDetail struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []FieldDetail `json:"details"`
}

// FieldDetail points at one invalid input field.
type FieldDetail struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

type dataBody struct {
	Data any `json:"data"`
	Meta any `json:"meta,omitempty"`
}

func respondJSON(w http.ResponseWriter, code int, data any) {
	writeJSON(w, code, dataBody{Data: data})
}

// respondError maps err to a status and code via errors.go and writes the envelope.
// Unexpected errors are logged once here, never echoed to the client.
func respondError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	m := mapError(err)
	if m.status >= http.StatusInternalServerError {
		logger.ErrorContext(r.Context(), "request failed",
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("error", err.Error()))
	}
	details := []FieldDetail{}
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		for _, f := range ve.Fields {
			details = append(details, FieldDetail{Field: f.Field, Issue: f.Issue})
		}
	}
	writeJSON(w, m.status, errorBody{
		Error:     errorDetail{Code: m.code, Message: m.message, Details: details},
		RequestID: middleware.GetReqID(r.Context()),
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v) // write error means the client went away; nothing to do
}
