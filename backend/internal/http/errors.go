package http

import (
	"errors"
	"net/http"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type mappedError struct {
	status  int
	code    string
	message string
}

// errorTable is the single sentinel → HTTP mapping (AGENTS.md §5). Order matters only if a
// wrapped error matches two sentinels; keep entries disjoint.
var errorTable = []struct {
	target error
	mapped mappedError
}{
	{domain.ErrInvalidInput, mappedError{http.StatusBadRequest, "VALIDATION_FAILED", "request is invalid"}},
	{domain.ErrUnauthorized, mappedError{http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"}},
	{domain.ErrForbidden, mappedError{http.StatusForbidden, "FORBIDDEN", "not allowed"}},
	{domain.ErrNotFound, mappedError{http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found"}},
	{domain.ErrAlreadyExists, mappedError{http.StatusConflict, "ALREADY_EXISTS", "resource already exists"}},
	{domain.ErrConflict, mappedError{http.StatusConflict, "CONFLICT", "request conflicts with current state"}},
	{domain.ErrPayloadTooBig, mappedError{http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "payload exceeds 256 KB"}},
	{domain.ErrBlockedHost, mappedError{http.StatusUnprocessableEntity, "BLOCKED_HOST", "endpoint host is not allowed"}},
	{domain.ErrRateLimited, mappedError{http.StatusTooManyRequests, "RATE_LIMITED", "too many requests"}},
	{domain.ErrUnavailable, mappedError{http.StatusServiceUnavailable, "UNAVAILABLE", "service temporarily unavailable"}},
}

var internalError = mappedError{http.StatusInternalServerError, "INTERNAL", "unexpected error"}

func mapError(err error) mappedError {
	for _, e := range errorTable {
		if errors.Is(err, e.target) {
			return e.mapped
		}
	}
	return internalError
}
