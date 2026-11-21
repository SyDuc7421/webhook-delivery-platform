//go:build integration

package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/testenv"
)

func TestUserRepository(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	tx := postgres.NewTransactor(pool)
	orgs := postgres.NewOrgRepo(pool)
	users := postgres.NewUserRepo(pool)

	org := domain.Org{ID: platform.NewID(), Name: "Acme"}
	user := domain.User{ID: platform.NewID(), OrgID: org.ID, Email: "alice@example.com", PasswordHash: "h", Role: domain.RoleOwner}

	t.Run("create org and user in one transaction", func(t *testing.T) {
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			if err := orgs.Create(ctx, org); err != nil {
				return err
			}
			return users.Create(ctx, user)
		})
		if err != nil {
			t.Fatalf("within tx: %v", err)
		}
		got, err := users.GetByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("get by email: %v", err)
		}
		if got.ID != user.ID || got.OrgID != org.ID || got.Role != domain.RoleOwner {
			t.Errorf("user = %+v", got)
		}
		if _, err := users.Get(ctx, user.ID); err != nil {
			t.Errorf("get: %v", err)
		}
		if o, err := orgs.Get(ctx, org.ID); err != nil || o.Name != "Acme" {
			t.Errorf("org = %+v, err = %v", o, err)
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		dup := user
		dup.ID = platform.NewID()
		if err := users.Create(ctx, dup); !errors.Is(err, domain.ErrAlreadyExists) {
			t.Errorf("err = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("rollback on error", func(t *testing.T) {
		ghost := domain.Org{ID: platform.NewID(), Name: "Ghost"}
		boom := errors.New("boom")
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			if err := orgs.Create(ctx, ghost); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
		if _, err := orgs.Get(ctx, ghost.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("rolled-back org: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("missing user", func(t *testing.T) {
		if _, err := users.GetByEmail(ctx, "missing@x.io"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}
