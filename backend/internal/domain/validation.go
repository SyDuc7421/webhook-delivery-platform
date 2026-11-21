package domain

import (
	"errors"
	"strings"
)

// ErrUnprocessable means the input is well-formed but refers to things that do not exist or
// conflict with rules (unknown consumer, unknown event type). Mapped to 422.
var ErrUnprocessable = errors.New("unprocessable")

// FieldError points at one invalid input field.
type FieldError struct {
	Field string
	Issue string
}

// ValidationError carries per-field details. It matches ErrInvalidInput (400) unless Cause
// names another sentinel, e.g. ErrUnprocessable (422).
type ValidationError struct {
	Cause  error
	Fields []FieldError
}

// Invalid is shorthand for a single-field 400.
func Invalid(field, issue string) *ValidationError {
	return &ValidationError{Fields: []FieldError{{Field: field, Issue: issue}}}
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, f.Field+" "+f.Issue)
	}
	return e.cause().Error() + ": " + strings.Join(parts, "; ")
}

// Is lets errors.Is(err, ErrInvalidInput) (or the Cause) match.
func (e *ValidationError) Is(target error) bool { return target == e.cause() }

func (e *ValidationError) cause() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrInvalidInput
}
