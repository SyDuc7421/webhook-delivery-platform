package domain

import (
	"time"

	"github.com/google/uuid"
)

// APIKey authenticates an org's backend on the ingest API. The full key is shown once at
// creation; only its hash and a short visible prefix are stored (FR-K1, FR-K2).
type APIKey struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Name      string
	Prefix    string
	CreatedAt time.Time
	RevokedAt *time.Time
}
