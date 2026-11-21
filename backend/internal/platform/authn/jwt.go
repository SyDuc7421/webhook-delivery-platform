package authn

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

const (
	issuer   = "webhook-platform"
	audience = "dashboard"
)

// Clock is the time source; platform.SystemClock satisfies it.
type Clock interface{ Now() time.Time }

// Claims is what an access token asserts about its holder.
type Claims struct {
	UserID    uuid.UUID
	OrgID     uuid.UUID
	Role      string
	ExpiresAt time.Time
}

// JWT issues and verifies HS256 dashboard access tokens.
type JWT struct {
	secret []byte
	ttl    time.Duration
	clock  Clock
}

// NewJWT returns a JWT issuer/verifier. secret must be at least 32 bytes (enforced by config).
func NewJWT(secret []byte, ttl time.Duration, clock Clock) *JWT {
	return &JWT{secret: secret, ttl: ttl, clock: clock}
}

type tokenClaims struct {
	OrgID string `json:"org"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// Issue signs an access token for c and returns it with its expiry.
func (j *JWT) Issue(c Claims) (string, time.Time, error) {
	now := j.clock.Now()
	exp := now.Add(j.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims{
		OrgID: c.OrgID.String(),
		Role:  c.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   c.UserID.String(),
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	})
	s, err := tok.SignedString(j.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return s, exp, nil
}

// Parse verifies the token and returns its claims. Every failure wraps domain.ErrUnauthorized.
func (j *JWT) Parse(token string) (Claims, error) {
	var tc tokenClaims
	_, err := jwt.ParseWithClaims(token, &tc, func(*jwt.Token) (any, error) { return j.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(audience),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(j.clock.Now),
	)
	if err != nil {
		return Claims{}, fmt.Errorf("access token: %w: %w", domain.ErrUnauthorized, err)
	}
	userID, uerr := uuid.Parse(tc.Subject)
	orgID, oerr := uuid.Parse(tc.OrgID)
	if err := errors.Join(uerr, oerr); err != nil {
		return Claims{}, fmt.Errorf("access token ids: %w: %w", domain.ErrUnauthorized, err)
	}
	return Claims{UserID: userID, OrgID: orgID, Role: tc.Role, ExpiresAt: tc.ExpiresAt.Time}, nil
}
