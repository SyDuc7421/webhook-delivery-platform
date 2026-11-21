//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/testenv"
)

// seedOrg creates an org with an owner user and returns both ids.
func seedOrg(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) (orgID, userID uuid.UUID) {
	t.Helper()
	orgID, userID = platform.NewID(), platform.NewID()
	if err := postgres.NewOrgRepo(pool).Create(ctx, domain.Org{ID: orgID, Name: name}); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	u := domain.User{ID: userID, OrgID: orgID, Email: name + "@example.com", PasswordHash: "h", Role: domain.RoleOwner}
	if err := postgres.NewUserRepo(pool).Create(ctx, u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return orgID, userID
}

func TestCatalogRepositories(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	orgA, userA := seedOrg(ctx, t, pool, "orga")
	orgB, _ := seedOrg(ctx, t, pool, "orgb")
	keys := postgres.NewAPIKeyRepo(pool)
	types := postgres.NewEventTypeRepo(pool)
	consumers := postgres.NewConsumerRepo(pool)
	endpoints := postgres.NewEndpointRepo(pool)

	t.Run("event type names are unique per org", func(t *testing.T) {
		et := domain.EventType{ID: platform.NewID(), OrgID: orgA, Name: "payment.paid", ExamplePayload: json.RawMessage(`{"a":1}`)}
		if err := types.Create(ctx, et); err != nil {
			t.Fatalf("create: %v", err)
		}
		dup := et
		dup.ID = platform.NewID()
		if err := types.Create(ctx, dup); !errors.Is(err, domain.ErrAlreadyExists) {
			t.Errorf("same org duplicate: err = %v, want ErrAlreadyExists", err)
		}
		other := et
		other.ID, other.OrgID = platform.NewID(), orgB
		if err := types.Create(ctx, other); err != nil {
			t.Errorf("same name in another org must work: %v", err)
		}
		got, err := types.Get(ctx, orgA, et.ID)
		if err != nil || string(got.ExamplePayload) != `{"a": 1}` {
			t.Errorf("get = %+v, %v", got, err)
		}
	})

	t.Run("event type without example payload round-trips as empty", func(t *testing.T) {
		et := domain.EventType{ID: platform.NewID(), OrgID: orgA, Name: "no.example"}
		if err := types.Create(ctx, et); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := types.Get(ctx, orgA, et.ID)
		if err != nil || len(got.ExamplePayload) != 0 {
			t.Errorf("example payload = %q, err = %v, want empty", got.ExamplePayload, err)
		}
	})

	t.Run("get by names", func(t *testing.T) {
		got, err := types.GetByNames(ctx, orgA, []string{"payment.paid", "no.example", "missing"})
		if err != nil || len(got) != 2 {
			t.Errorf("get by names = %d rows, err = %v, want 2", len(got), err)
		}
	})

	t.Run("get with another org's id is not found", func(t *testing.T) {
		et := domain.EventType{ID: platform.NewID(), OrgID: orgA, Name: "private"}
		if err := types.Create(ctx, et); err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := types.Get(ctx, orgB, et.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
		if err := types.Delete(ctx, orgB, et.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete by other org err = %v, want ErrNotFound", err)
		}
		if _, err := types.Get(ctx, orgA, et.ID); err != nil {
			t.Errorf("row must survive the other org's delete: %v", err)
		}
	})

	cons := domain.Consumer{ID: platform.NewID(), OrgID: orgA, UID: "shop_42", Name: "Shop"}
	t.Run("consumer uid is unique per org", func(t *testing.T) {
		if err := consumers.Create(ctx, cons); err != nil {
			t.Fatalf("create: %v", err)
		}
		dup := cons
		dup.ID = platform.NewID()
		if err := consumers.Create(ctx, dup); !errors.Is(err, domain.ErrAlreadyExists) {
			t.Errorf("err = %v, want ErrAlreadyExists", err)
		}
		got, err := consumers.GetByUID(ctx, orgA, "shop_42")
		if err != nil || got.ID != cons.ID {
			t.Errorf("get by uid = %+v, %v", got, err)
		}
		if _, err := consumers.GetByUID(ctx, orgB, "shop_42"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("other org by uid err = %v, want ErrNotFound", err)
		}
	})

	t.Run("deleting a consumer cascades to its endpoints", func(t *testing.T) {
		ep := domain.Endpoint{
			ID: platform.NewID(), OrgID: orgA, ConsumerID: cons.ID, URL: "https://example.com/hook",
			EventTypes: []string{"payment.paid"}, Enabled: true, SecretCiphertext: []byte("sealed"),
		}
		if err := endpoints.Create(ctx, ep); err != nil {
			t.Fatalf("create endpoint: %v", err)
		}
		got, err := endpoints.Get(ctx, orgA, ep.ID)
		if err != nil || got.URL != ep.URL || !got.Enabled || len(got.EventTypes) != 1 || string(got.SecretCiphertext) != "sealed" {
			t.Fatalf("endpoint = %+v, %v", got, err)
		}
		if err := endpoints.UpdateSecret(ctx, orgA, ep.ID, []byte("sealed-2")); err != nil {
			t.Fatalf("update secret: %v", err)
		}
		got, _ = endpoints.Get(ctx, orgA, ep.ID)
		if string(got.SecretCiphertext) != "sealed-2" {
			t.Errorf("secret not updated")
		}
		if err := endpoints.UpdateSecret(ctx, orgB, ep.ID, []byte("hijack")); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("other org update secret err = %v, want ErrNotFound", err)
		}
		if err := consumers.Delete(ctx, orgA, cons.ID); err != nil {
			t.Fatalf("delete consumer: %v", err)
		}
		if _, err := endpoints.Get(ctx, orgA, ep.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("endpoint after consumer delete err = %v, want ErrNotFound", err)
		}
	})

	t.Run("revoked api key is not active", func(t *testing.T) {
		k := domain.APIKey{ID: platform.NewID(), OrgID: orgA, Name: "ci", Prefix: "whk_abcdefgh"}
		hash := []byte("0123456789abcdef0123456789abcdef")
		if err := keys.Create(ctx, k, hash, userA); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := keys.GetActiveByHash(ctx, hash)
		if err != nil || got.ID != k.ID || got.OrgID != orgA {
			t.Fatalf("active = %+v, %v", got, err)
		}
		if err := keys.Revoke(ctx, orgB, k.ID, time.Now()); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("other org revoke err = %v, want ErrNotFound", err)
		}
		if _, err := keys.GetActiveByHash(ctx, hash); err != nil {
			t.Errorf("must still be active after the other org's revoke attempt: %v", err)
		}
		if err := keys.Revoke(ctx, orgA, k.ID, time.Now()); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, err := keys.GetActiveByHash(ctx, hash); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("revoked key err = %v, want ErrNotFound", err)
		}
	})

	t.Run("keyset pagination returns every row once, newest first", func(t *testing.T) {
		const total = 25
		want := make([]uuid.UUID, 0, total)
		for i := range total {
			et := domain.EventType{ID: platform.NewID(), OrgID: orgB, Name: "page." + string(rune('a'+i))}
			if err := types.Create(ctx, et); err != nil {
				t.Fatalf("create %d: %v", i, err)
			}
			want = append(want, et.ID)
		}
		var got []domain.EventType
		var after *domain.Cursor
		for range 10 { // safety bound
			page, err := types.List(ctx, orgB, domain.Page{Limit: 10, After: after})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			got = append(got, page...)
			if len(page) < 10 {
				break
			}
			last := page[len(page)-1]
			after = &domain.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}
		// orgB also owns the "payment.paid" row created earlier: 26 rows total.
		if len(got) != total+1 {
			t.Fatalf("got %d rows, want %d", len(got), total+1)
		}
		seen := map[uuid.UUID]bool{}
		for _, et := range got {
			if seen[et.ID] {
				t.Fatalf("row %s returned twice", et.ID)
			}
			seen[et.ID] = true
		}
		for _, id := range want {
			if !seen[id] {
				t.Errorf("row %s missing", id)
			}
		}
		// Order is (created_at, id) descending; v7 ids inside one millisecond are not ordered.
		for i := 1; i < len(got); i++ {
			prev, cur := got[i-1], got[i]
			newer := prev.CreatedAt.After(cur.CreatedAt) || (prev.CreatedAt.Equal(cur.CreatedAt) && prev.ID.String() > cur.ID.String())
			if !newer {
				t.Fatalf("order broken at %d: %v/%s then %v/%s", i, prev.CreatedAt, prev.ID, cur.CreatedAt, cur.ID)
			}
		}
	})
}
