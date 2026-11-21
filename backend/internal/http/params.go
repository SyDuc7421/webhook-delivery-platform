package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// idParam reads a UUID path parameter. A malformed id cannot exist, so it is a 404.
func idParam(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, fmt.Errorf("path parameter %s: %w", name, domain.ErrNotFound)
	}
	return id, nil
}

// optionalJSON tells "field absent" (Set=false) from "field null" (Set=true, Value=null) from a
// value. Plain pointers cannot: encoding/json maps null to a nil pointer.
type optionalJSON struct {
	Set   bool
	Value json.RawMessage
}

// UnmarshalJSON implements json.Unmarshaler; it is only called when the field is present.
func (o *optionalJSON) UnmarshalJSON(b []byte) error {
	o.Set = true
	o.Value = append(o.Value[:0], b...)
	return nil
}

// isNull reports whether the present value is JSON null.
func (o optionalJSON) isNull() bool { return string(o.Value) == "null" }

// rawOrNil returns a raw JSON value for create requests: null and absent both mean "none".
func rawOrNil(b json.RawMessage) json.RawMessage {
	if string(b) == "null" {
		return nil
	}
	return b
}
