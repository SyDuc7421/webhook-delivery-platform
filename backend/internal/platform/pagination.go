package platform

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type cursorWire struct {
	T string    `json:"t"`
	I uuid.UUID `json:"i"`
}

// EncodeCursor makes the keyset position opaque to clients (FR-W3). The timestamp keeps
// microsecond precision, which is what Postgres stores.
func EncodeCursor(c domain.Cursor) string {
	b, _ := json.Marshal(cursorWire{T: c.CreatedAt.UTC().Format(time.RFC3339Nano), I: c.ID}) //nolint:errchkjson // plain struct cannot fail to marshal
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses a cursor from a query string. Any malformed input is a 400 on `cursor`.
func DecodeCursor(s string) (domain.Cursor, error) {
	bad := domain.Invalid("cursor", "is not a valid cursor")
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.Cursor{}, bad
	}
	var w cursorWire
	if jerr := json.Unmarshal(raw, &w); jerr != nil || w.I == uuid.Nil {
		return domain.Cursor{}, bad
	}
	t, err := time.Parse(time.RFC3339Nano, w.T)
	if err != nil {
		return domain.Cursor{}, bad
	}
	return domain.Cursor{CreatedAt: t, ID: w.I}, nil
}

// ParseLimit reads the `limit` query parameter: default 20, 1..100.
func ParseLimit(s string) (int, error) {
	if s == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxLimit {
		return 0, domain.Invalid("limit", "must be an integer between 1 and 100")
	}
	return n, nil
}
