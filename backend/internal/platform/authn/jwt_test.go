package authn

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

var secret = []byte("jwt-test-secret-that-is-32-bytes!")

func TestJWT(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	j := NewJWT(secret, 15*time.Minute, clock)
	want := Claims{UserID: uuid.New(), OrgID: uuid.New(), Role: "owner"}

	tok, exp, err := j.Issue(want)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !exp.Equal(clock.now.Add(15 * time.Minute)) {
		t.Errorf("expiresAt = %v", exp)
	}
	got, err := j.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.UserID != want.UserID || got.OrgID != want.OrgID || got.Role != want.Role {
		t.Errorf("claims = %+v, want %+v", got, want)
	}

	t.Run("other secret", func(t *testing.T) {
		t.Parallel()
		other := NewJWT([]byte("another-secret-that-is-32-bytes!!"), time.Minute, clock)
		if _, err := other.Parse(tok); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		later := NewJWT(secret, 15*time.Minute, &fakeClock{now: clock.now.Add(16 * time.Minute)})
		if _, err := later.Parse(tok); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("alg none", func(t *testing.T) {
		t.Parallel()
		none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
			"sub": want.UserID.String(), "org": want.OrgID.String(), "role": "owner",
			"aud": "dashboard", "iss": "webhook-platform", "exp": clock.now.Add(time.Hour).Unix(),
		})
		s, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("sign none: %v", err)
		}
		if _, err := j.Parse(s); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		t.Parallel()
		bad := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": want.UserID.String(), "org": want.OrgID.String(), "role": "owner",
			"aud": "ingest", "iss": "webhook-platform", "exp": clock.now.Add(time.Hour).Unix(),
		})
		s, err := bad.SignedString(secret)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if _, err := j.Parse(s); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("garbage", func(t *testing.T) {
		t.Parallel()
		if _, err := j.Parse("not.a.jwt"); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})
}
