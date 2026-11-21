package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
)

var discard = slog.New(slog.NewJSONHandler(io.Discard, nil))

type authFixture struct {
	svc     *AuthService
	jwt     *authn.JWT
	users   *fakeUsers
	orgs    *fakeOrgs
	refresh *fakeRefresh
}

func newAuthFixture() authFixture {
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	j := authn.NewJWT([]byte("service-test-secret-at-least-32-bytes"), 15*time.Minute, clock)
	f := authFixture{jwt: j, users: newFakeUsers(), orgs: newFakeOrgs(), refresh: newFakeRefresh()}
	f.svc = NewAuthService(AuthDeps{
		Tx: fakeTx{}, Orgs: f.orgs, Users: f.users, Refresh: f.refresh,
		JWT: j, RefreshTTL: 168 * time.Hour, Clock: clock, Logger: discard,
	})
	return f
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	m := map[string]string{}
	for _, f := range ve.Fields {
		m[f.Field] = f.Issue
	}
	return m
}

const goodPassword = "correct horse battery"

func TestRegister(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creates org and owner with normalised email", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture()
		s, err := f.svc.Register(ctx, RegisterInput{Email: " Alice@Example.com ", Password: goodPassword, OrgName: "Acme"})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if s.User.Email != "alice@example.com" || s.User.Role != domain.RoleOwner || s.User.OrgID != s.Org.ID {
			t.Errorf("user = %+v", s.User)
		}
		if s.Org.Name != "Acme" {
			t.Errorf("org = %+v", s.Org)
		}
		claims, err := f.jwt.Parse(s.AccessToken)
		if err != nil || claims.UserID != s.User.ID || claims.OrgID != s.Org.ID || claims.Role != "owner" {
			t.Errorf("access token claims = %+v, err = %v", claims, err)
		}
		if s.RefreshToken == "" || !f.refresh.has(authn.HashOpaqueToken(s.RefreshToken)) {
			t.Error("refresh token must be returned and stored by hash")
		}
		if s.User.PasswordHash == goodPassword || s.User.PasswordHash == "" {
			t.Error("password must be stored hashed")
		}
	})

	t.Run("duplicate email in any case", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture()
		if _, err := f.svc.Register(ctx, RegisterInput{Email: "alice@example.com", Password: goodPassword, OrgName: "A"}); err != nil {
			t.Fatalf("first: %v", err)
		}
		_, err := f.svc.Register(ctx, RegisterInput{Email: "ALICE@example.com", Password: goodPassword, OrgName: "B"})
		if !errors.Is(err, domain.ErrAlreadyExists) {
			t.Errorf("err = %v, want ErrAlreadyExists", err)
		}
	})

	invalid := []struct {
		name  string
		in    RegisterInput
		field string
	}{
		{"short password", RegisterInput{Email: "a@b.co", Password: "elevenchars", OrgName: "A"}, "password"},
		{"long password", RegisterInput{Email: "a@b.co", Password: string(make([]byte, 129)), OrgName: "A"}, "password"},
		{"bad email", RegisterInput{Email: "not-an-email", Password: goodPassword, OrgName: "A"}, "email"},
		{"empty org", RegisterInput{Email: "a@b.co", Password: goodPassword, OrgName: "  "}, "org_name"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := newAuthFixture().svc.Register(ctx, tt.in)
			if _, ok := fieldsOf(t, err)[tt.field]; !ok {
				t.Errorf("err = %v, want field %s", err, tt.field)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newAuthFixture()
	if _, err := f.svc.Register(ctx, RegisterInput{Email: "bob@example.com", Password: goodPassword, OrgName: "Bob Co"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	s, err := f.svc.Login(ctx, " BOB@example.com", goodPassword)
	if err != nil || s.User.Email != "bob@example.com" || s.AccessToken == "" {
		t.Fatalf("login = %+v, %v", s.User, err)
	}

	_, wrongPw := f.svc.Login(ctx, "bob@example.com", "wrong password!!")
	_, unknown := f.svc.Login(ctx, "nobody@example.com", goodPassword)
	if !errors.Is(wrongPw, domain.ErrUnauthorized) || !errors.Is(unknown, domain.ErrUnauthorized) {
		t.Fatalf("wrongPw = %v, unknown = %v; both must be ErrUnauthorized", wrongPw, unknown)
	}
	if wrongPw.Error() != unknown.Error() {
		t.Errorf("messages differ (%q vs %q): reveals which emails exist", wrongPw, unknown)
	}
}

func TestRefreshAndLogout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("refresh rotates the token", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture()
		s1, err := f.svc.Register(ctx, RegisterInput{Email: "c@example.com", Password: goodPassword, OrgName: "C"})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		s2, err := f.svc.Refresh(ctx, s1.RefreshToken)
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if s2.RefreshToken == s1.RefreshToken || s2.User.ID != s1.User.ID || s2.Org.ID != s1.Org.ID {
			t.Errorf("rotated session = %+v", s2)
		}
		if _, err := f.svc.Refresh(ctx, s1.RefreshToken); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("reused token err = %v, want ErrUnauthorized", err)
		}
		if _, err := f.svc.Refresh(ctx, s2.RefreshToken); err != nil {
			t.Errorf("new token must work: %v", err)
		}
	})

	t.Run("refresh store down rejects", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture()
		s, err := f.svc.Register(ctx, RegisterInput{Email: "d@example.com", Password: goodPassword, OrgName: "D"})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		f.refresh.down = true
		if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized (users log in again)", err)
		}
	})

	t.Run("logout kills the refresh token", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture()
		s, err := f.svc.Register(ctx, RegisterInput{Email: "e@example.com", Password: goodPassword, OrgName: "E"})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		if err := f.svc.Logout(ctx, s.RefreshToken); err != nil {
			t.Fatalf("logout: %v", err)
		}
		if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("me", func(t *testing.T) {
		t.Parallel()
		f := newAuthFixture()
		s, err := f.svc.Register(ctx, RegisterInput{Email: "f@example.com", Password: goodPassword, OrgName: "F"})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		u, o, err := f.svc.Me(ctx, s.User.ID)
		if err != nil || u.ID != s.User.ID || o.ID != s.Org.ID {
			t.Errorf("me = %+v %+v %v", u, o, err)
		}
	})
}

func TestLoginAnswersUnavailableWhenHashingIsSaturated(t *testing.T) {
	t.Parallel()
	f := newAuthFixture()
	limiter := authn.NewLimiter(1)
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	svc := NewAuthService(AuthDeps{
		Tx: fakeTx{}, Orgs: f.orgs, Users: f.users, Refresh: f.refresh, JWT: f.jwt,
		RefreshTTL: time.Hour, Clock: clock, Logger: discard, Hashing: limiter,
	})
	started, release := make(chan struct{}), make(chan struct{})
	go func() { _ = limiter.Do(context.Background(), func() error { close(started); <-release; return nil }) }()
	<-started
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := svc.Login(ctx, "nobody@example.com", goodPassword); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("login err = %v, want ErrUnavailable (argon2 memory is bounded, extra requests wait or fail, never pile up)", err)
	}
	if _, err := svc.Register(ctx, RegisterInput{Email: "a@b.co", Password: goodPassword, OrgName: "A"}); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("register err = %v, want ErrUnavailable", err)
	}
}
