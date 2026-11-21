package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EventType is a named kind of event, e.g. "payment.paid" (FR-T1).
type EventType struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	Name           string
	Description    string
	ExamplePayload json.RawMessage // optional; used by "send test event" (FR-T2); never schema-validated (FR-T3)
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Consumer is one of the org's customers, identified by the org's own uid (FR-E1).
type Consumer struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	UID       string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Endpoint is a URL owned by a consumer (FR-E2). An empty EventTypes list means "all".
type Endpoint struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	ConsumerID       uuid.UUID
	URL              string
	Description      string
	EventTypes       []string
	Enabled          bool
	SecretCiphertext []byte // AES-256-GCM sealed signing secret; never serialised to clients
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Cursor is the keyset position of the last row of a page. Lists order by (created_at, id)
// descending, which is what makes the cursor stable under inserts.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Page selects a slice of a list: Limit rows after the cursor (nil = first page).
type Page struct {
	Limit int
	After *Cursor
}
