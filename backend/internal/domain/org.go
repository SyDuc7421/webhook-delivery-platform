package domain

import (
	"time"

	"github.com/google/uuid"
)

// Role is a user's permission level inside an org.
type Role string

// Roles (FR-A1).
const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

// Org is a tenant: the SaaS company using the platform.
type Org struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

// User is a person who logs into the dashboard. Belongs to exactly one org.
type User struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	Email        string
	PasswordHash string
	Role         Role
	CreatedAt    time.Time
}
