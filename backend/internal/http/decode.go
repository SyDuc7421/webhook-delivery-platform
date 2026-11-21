package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

// decodeJSON reads one JSON value into dst (unknown fields and trailing data rejected) and
// runs its `validate` tags. Errors are domain sentinels so respondError maps them.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return jsonDecodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return fmt.Errorf("body over %d bytes: %w", mbe.Limit, domain.ErrPayloadTooBig)
		}
		return domain.Invalid("body", "must contain a single JSON value")
	}
	return validateStruct(dst)
}

func jsonDecodeError(err error) error {
	var mbe *http.MaxBytesError
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &mbe):
		return fmt.Errorf("body over %d bytes: %w", mbe.Limit, domain.ErrPayloadTooBig)
	case errors.Is(err, io.EOF):
		return domain.Invalid("body", "is required")
	case errors.As(err, &typ):
		return domain.Invalid(typ.Field, "has the wrong type")
	case errors.As(err, &syn), errors.Is(err, io.ErrUnexpectedEOF):
		return domain.Invalid("body", "malformed JSON")
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return domain.Invalid("body", strings.TrimPrefix(err.Error(), "json: "))
	default:
		return domain.Invalid("body", "malformed JSON")
	}
}

func validateStruct(dst any) error {
	err := validate.Struct(dst)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return fmt.Errorf("validate: %w", err)
	}
	ve := &domain.ValidationError{}
	for _, fe := range verrs {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: fieldPath(fe), Issue: issueFor(fe)})
	}
	return ve
}

// fieldPath drops the top-level struct name: "signupReq.email" → "email".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if _, rest, ok := strings.Cut(ns, "."); ok {
		return rest
	}
	return fe.Field()
}

func issueFor(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	default:
		return "is invalid"
	}
}
