package platform

import (
	"time"

	"github.com/google/uuid"
)

// NewID returns a UUIDv7: time-ordered, so keyset pagination and B-tree inserts stay cheap.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Clock is injected wherever a service needs the current time (docs/TESTING.md).
type Clock interface {
	Now() time.Time
}

// SystemClock is the production Clock.
type SystemClock struct{}

// Now returns the current time in UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }
