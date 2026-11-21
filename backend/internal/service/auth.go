package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
)

// errBadCredentials is the single error for unknown email and wrong password, so responses
// do not reveal which emails are registered.
var errBadCredentials = fmt.Errorf("invalid email or password: %w", domain.ErrUnauthorized)

// AuthDeps are the AuthService's collaborators.
type AuthDeps struct {
	Tx         domain.Transactor
	Orgs       domain.OrgRepository
	Users      domain.UserRepository
	Refresh    domain.RefreshStore
	JWT        *authn.JWT
	RefreshTTL time.Duration
	Clock      platform.Clock
	Logger     *slog.Logger
	// Hashing bounds concurrent argon2id work; nil means one slot per CPU (at least 2).
	Hashing *authn.Limiter
}

// AuthService implements registration and dashboard sessions (FR-A1, FR-A2).
type AuthService struct {
	d AuthDeps

	dummyOnce sync.Once
	dummyHash string
}

// NewAuthService returns an AuthService.
func NewAuthService(d AuthDeps) *AuthService {
	if d.Hashing == nil {
		d.Hashing = authn.NewLimiter(max(2, runtime.GOMAXPROCS(0)))
	}
	return &AuthService{d: d}
}

func (s *AuthService) hashPassword(ctx context.Context, pw string) (string, error) {
	var hash string
	err := s.d.Hashing.Do(ctx, func() error {
		var herr error
		hash, herr = authn.HashPassword(pw)
		return herr
	})
	return hash, err
}

func (s *AuthService) verifyPassword(ctx context.Context, encoded, pw string) (bool, error) {
	var ok bool
	err := s.d.Hashing.Do(ctx, func() error {
		var verr error
		ok, verr = authn.VerifyPassword(encoded, pw)
		return verr
	})
	return ok, err
}

// RegisterInput is a new account: the user becomes owner of a new org.
type RegisterInput struct {
	Email    string
	Password string
	OrgName  string
}

// Session is what a successful register, login or refresh returns.
type Session struct {
	User            domain.User
	Org             domain.Org
	AccessToken     string
	AccessExpiresAt time.Time
	RefreshToken    string
}

// Register creates an org and its owner in one transaction and opens a session (FR-A1).
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (Session, error) {
	email := normalizeEmail(in.Email)
	orgName := strings.TrimSpace(in.OrgName)
	if err := validateRegistration(email, in.Password, orgName); err != nil {
		return Session{}, err
	}
	hash, err := s.hashPassword(ctx, in.Password)
	if err != nil {
		return Session{}, fmt.Errorf("hash password: %w", err)
	}
	now := s.d.Clock.Now()
	org := domain.Org{ID: platform.NewID(), Name: orgName, CreatedAt: now}
	user := domain.User{ID: platform.NewID(), OrgID: org.ID, Email: email, PasswordHash: hash, Role: domain.RoleOwner, CreatedAt: now}

	err = s.d.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if cerr := s.d.Orgs.Create(ctx, org); cerr != nil {
			return cerr
		}
		return s.d.Users.Create(ctx, user)
	})
	if err != nil {
		return Session{}, fmt.Errorf("register: %w", err)
	}
	return s.open(ctx, user, org)
}

// Login checks credentials and opens a session (FR-A2).
func (s *AuthService) Login(ctx context.Context, email, password string) (Session, error) {
	user, err := s.d.Users.GetByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, domain.ErrNotFound) {
		// Spend the same hashing time as a real check so timing does not reveal the email.
		if _, verr := s.verifyPassword(ctx, s.dummy(), password); verr != nil && errors.Is(verr, domain.ErrUnavailable) {
			return Session{}, fmt.Errorf("login: %w", verr)
		}
		return Session{}, errBadCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("login: %w", err)
	}
	ok, err := s.verifyPassword(ctx, user.PasswordHash, password)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return Session{}, errBadCredentials
	}
	org, err := s.d.Orgs.Get(ctx, user.OrgID)
	if err != nil {
		return Session{}, fmt.Errorf("login org: %w", err)
	}
	return s.open(ctx, user, org)
}

// Refresh trades a refresh token for a new session; the old token stops working.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	userID, err := s.d.Refresh.Consume(ctx, authn.HashOpaqueToken(refreshToken))
	if errors.Is(err, domain.ErrUnavailable) {
		s.d.Logger.WarnContext(ctx, "refresh store unavailable, rejecting refresh", slog.String("error", err.Error()))
		return Session{}, fmt.Errorf("refresh: %w", domain.ErrUnauthorized)
	}
	if err != nil {
		return Session{}, fmt.Errorf("refresh: %w", err)
	}
	user, org, err := s.Me(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return Session{}, fmt.Errorf("refresh for deleted user: %w", domain.ErrUnauthorized)
	}
	if err != nil {
		return Session{}, err
	}
	return s.open(ctx, user, org)
}

// Logout revokes a refresh token. Access tokens expire on their own (15 min).
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	if err := s.d.Refresh.Delete(ctx, authn.HashOpaqueToken(refreshToken)); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// Me returns the user and their org.
func (s *AuthService) Me(ctx context.Context, userID uuid.UUID) (domain.User, domain.Org, error) {
	user, err := s.d.Users.Get(ctx, userID)
	if err != nil {
		return domain.User{}, domain.Org{}, fmt.Errorf("me: %w", err)
	}
	org, err := s.d.Orgs.Get(ctx, user.OrgID)
	if err != nil {
		return domain.User{}, domain.Org{}, fmt.Errorf("me org: %w", err)
	}
	return user, org, nil
}

func (s *AuthService) open(ctx context.Context, user domain.User, org domain.Org) (Session, error) {
	access, exp, err := s.d.JWT.Issue(authn.Claims{UserID: user.ID, OrgID: org.ID, Role: string(user.Role)})
	if err != nil {
		return Session{}, err
	}
	refresh, hash, err := authn.NewOpaqueToken()
	if err != nil {
		return Session{}, err
	}
	if err := s.d.Refresh.Save(ctx, hash, user.ID, s.d.RefreshTTL); err != nil {
		return Session{}, fmt.Errorf("save refresh token: %w", err)
	}
	return Session{User: user, Org: org, AccessToken: access, AccessExpiresAt: exp, RefreshToken: refresh}, nil
}

func (s *AuthService) dummy() string {
	s.dummyOnce.Do(func() {
		h, err := authn.HashPassword("timing-equaliser-not-a-real-password")
		if err == nil {
			s.dummyHash = h
		}
	})
	return s.dummyHash
}

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func validateRegistration(email, password, orgName string) error {
	ve := &domain.ValidationError{}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "email", Issue: "must be a valid email"})
	}
	switch n := len(password); {
	case n < 12:
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "password", Issue: "must be at least 12 characters"})
	case n > 128:
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "password", Issue: "must be at most 128 characters"})
	}
	if orgName == "" || len(orgName) > 100 {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "org_name", Issue: "must be 1 to 100 characters"})
	}
	if len(ve.Fields) > 0 {
		return ve
	}
	return nil
}
