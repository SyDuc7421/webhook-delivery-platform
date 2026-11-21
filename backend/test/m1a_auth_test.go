//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/apitest"
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

func TestRefreshStore(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	client := redis.NewClient(env.Config.Redis)
	defer func() { _ = client.Close() }()
	store := redis.NewRefreshStore(client)
	userID := platform.NewID()

	t.Run("save then consume once", func(t *testing.T) {
		if err := store.Save(ctx, "hash-1", userID, time.Hour); err != nil {
			t.Fatalf("save: %v", err)
		}
		ttl, err := client.TTL(ctx, redis.RefreshTokenKey("hash-1")).Result()
		if err != nil || ttl <= 0 {
			t.Fatalf("ttl = %v, err = %v; every key must have a TTL", ttl, err)
		}
		got, err := store.Consume(ctx, "hash-1")
		if err != nil || got != userID {
			t.Fatalf("consume = %v, %v", got, err)
		}
		if _, err := store.Consume(ctx, "hash-1"); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("second consume err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("concurrent consume has one winner", func(t *testing.T) {
		if err := store.Save(ctx, "hash-race", userID, time.Hour); err != nil {
			t.Fatalf("save: %v", err)
		}
		var wins atomic.Int32
		var wg sync.WaitGroup
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := store.Consume(ctx, "hash-race"); err == nil {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		if wins.Load() != 1 {
			t.Errorf("winners = %d, want exactly 1", wins.Load())
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := store.Save(ctx, "hash-del", userID, time.Hour); err != nil {
			t.Fatalf("save: %v", err)
		}
		if err := store.Delete(ctx, "hash-del"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := store.Consume(ctx, "hash-del"); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("consume after delete err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("redis down is unavailable, not unauthorized", func(t *testing.T) {
		timeout := 5 * time.Second
		if err := env.Redis.Stop(ctx, &timeout); err != nil {
			t.Fatalf("stop redis: %v", err)
		}
		if _, err := store.Consume(ctx, "hash-1"); !errors.Is(err, domain.ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}
	})
}

type sessionBody struct {
	Data struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
		Org struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"org"`
		AccessToken  string `json:"access_token"`
		ExpiresAt    string `json:"expires_at"`
		RefreshToken string `json:"refresh_token"`
	} `json:"data"`
}

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return v
}

func TestAuthFlow(t *testing.T) {
	env := testenv.Start(t)
	c := apitest.New(t, env)
	ctx := t.Context()

	resp, body := c.Do(ctx, http.MethodPost, "/v1/auth/register", "", map[string]string{
		"email": " Alice@Example.com ", "password": "correct horse battery", "org_name": "Acme",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d %s", resp.StatusCode, body)
	}
	reg := decode[sessionBody](t, body)
	if reg.Data.User.Email != "alice@example.com" || reg.Data.User.Role != "owner" || reg.Data.Org.Name != "Acme" ||
		reg.Data.AccessToken == "" || reg.Data.RefreshToken == "" {
		t.Fatalf("register body = %s", body)
	}
	if strings.Contains(string(body), "password") {
		t.Errorf("response must not mention the password hash: %s", body)
	}

	resp, body = c.Do(ctx, http.MethodPost, "/v1/auth/register", "", map[string]string{
		"email": "ALICE@example.com", "password": "correct horse battery", "org_name": "Other",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate register = %d %s, want 409", resp.StatusCode, body)
	}

	resp, body = c.Do(ctx, http.MethodGet, "/v1/me", reg.Data.AccessToken, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), reg.Data.Org.ID) {
		t.Fatalf("me = %d %s", resp.StatusCode, body)
	}
	if r, _ := c.Do(ctx, http.MethodGet, "/v1/me", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("me without token = %d, want 401", r.StatusCode)
	}

	resp, body = c.Do(ctx, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": "alice@example.com", "password": "correct horse battery"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %s", resp.StatusCode, body)
	}
	login := decode[sessionBody](t, body)
	if r, _ := c.Do(ctx, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": "alice@example.com", "password": "wrong password!!"}); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad login = %d, want 401", r.StatusCode)
	}

	resp, body = c.Do(ctx, http.MethodPost, "/v1/auth/refresh", "", map[string]string{"refresh_token": login.Data.RefreshToken})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d %s", resp.StatusCode, body)
	}
	refreshed := decode[sessionBody](t, body)
	if refreshed.Data.RefreshToken == login.Data.RefreshToken {
		t.Error("refresh must rotate the token")
	}
	if r, _ := c.Do(ctx, http.MethodPost, "/v1/auth/refresh", "", map[string]string{"refresh_token": login.Data.RefreshToken}); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("reused refresh = %d, want 401", r.StatusCode)
	}

	if r, _ := c.Do(ctx, http.MethodPost, "/v1/auth/logout", "", map[string]string{"refresh_token": refreshed.Data.RefreshToken}); r.StatusCode != http.StatusNoContent {
		t.Errorf("logout = %d, want 204", r.StatusCode)
	}
	if r, _ := c.Do(ctx, http.MethodPost, "/v1/auth/refresh", "", map[string]string{"refresh_token": refreshed.Data.RefreshToken}); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("refresh after logout = %d, want 401", r.StatusCode)
	}

	t.Run("concurrent refresh has one winner", func(t *testing.T) {
		codes := make(chan int, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, _ := c.Do(ctx, http.MethodPost, "/v1/auth/refresh", "", map[string]string{"refresh_token": reg.Data.RefreshToken})
				codes <- resp.StatusCode
			}()
		}
		wg.Wait()
		close(codes)
		got := map[int]int{}
		for code := range codes {
			got[code]++
		}
		if got[http.StatusOK] != 1 || got[http.StatusUnauthorized] != 1 {
			t.Errorf("codes = %v, want one 200 and one 401", got)
		}
	})

	t.Run("validation details", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodPost, "/v1/auth/register", "", map[string]string{"email": "x", "password": "short", "org_name": "A"})
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"field":"password"`) || !strings.Contains(string(body), `"field":"email"`) {
			t.Errorf("register invalid = %d %s", resp.StatusCode, body)
		}
	})
}
