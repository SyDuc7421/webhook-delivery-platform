package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
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

// JSONStorageIssue returns "" if b, which must already be valid JSON, can be stored in a
// Postgres jsonb column, and a client-facing reason otherwise. json.Valid accepts three things
// jsonb rejects: invalid UTF-8, the \u0000 escape, and unpaired surrogate escapes. Without this
// check such a payload is a 500 on every retry.
func JSONStorageIssue(b []byte) string {
	const msg = "contains characters that cannot be stored: invalid UTF-8, \\u0000, or an unpaired surrogate escape"
	if !utf8.Valid(b) {
		return msg
	}
	inString := false
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '"':
			inString = !inString
		case inString && b[i] == '\\':
			next, ok := checkEscape(b, i+1)
			if !ok {
				return msg
			}
			i = next
		}
	}
	return ""
}

// checkEscape validates the escape whose character after the backslash is at b[at], and
// returns the index of the escape's last byte. Only \u escapes can be unstorable.
func checkEscape(b []byte, at int) (last int, ok bool) {
	if at >= len(b) {
		return at, false
	}
	if b[at] != 'u' {
		return at, true
	}
	r, ok := hex4(b, at+1)
	if !ok {
		return at, false
	}
	last = at + 4
	switch {
	case r == 0, r >= 0xDC00 && r <= 0xDFFF: // NUL, or a low surrogate on its own
		return last, false
	case r >= 0xD800 && r <= 0xDBFF: // high surrogate: a low surrogate escape must follow
		if last+6 >= len(b) || b[last+1] != '\\' || b[last+2] != 'u' {
			return last, false
		}
		low, lok := hex4(b, last+3)
		if !lok || low < 0xDC00 || low > 0xDFFF {
			return last, false
		}
		return last + 6, true
	}
	return last, true
}

// hex4 parses four hex digits at b[at:at+4].
func hex4(b []byte, at int) (rune, bool) {
	if at+4 > len(b) {
		return 0, false
	}
	var r rune
	for _, c := range b[at : at+4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}
