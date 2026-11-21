package domain

import "errors"

// Sentinel errors. Adapters wrap them; internal/http maps them to status codes once.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrConflict      = errors.New("conflict")
	ErrRateLimited   = errors.New("rate limited")
	ErrPayloadTooBig = errors.New("payload too large")
	ErrBlockedHost   = errors.New("blocked host")
	ErrUnavailable   = errors.New("dependency unavailable")
)
