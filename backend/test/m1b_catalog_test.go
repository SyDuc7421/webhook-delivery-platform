//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/authn"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/apitest"
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

// registerOwner registers an org through the API and returns its owner's access token and org id.
func registerOwner(ctx context.Context, t *testing.T, c *apitest.Client, email, orgName string) (token, orgID string) {
	t.Helper()
	resp, body := c.Do(ctx, http.MethodPost, "/v1/auth/register", "", map[string]string{
		"email": email, "password": "correct horse battery", "org_name": orgName,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s = %d %s", email, resp.StatusCode, body)
	}
	s := decode[sessionBody](t, body)
	return s.Data.AccessToken, s.Data.Org.ID
}

// memberToken adds a member to the org directly in the database and logs them in.
func memberToken(ctx context.Context, t *testing.T, c *apitest.Client, pool *pgxpool.Pool, orgID, email string) string {
	t.Helper()
	hash, err := authn.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := domain.User{ID: platform.NewID(), OrgID: uuid.MustParse(orgID), Email: email, PasswordHash: hash, Role: domain.RoleMember}
	if err := postgres.NewUserRepo(pool).Create(ctx, u); err != nil {
		t.Fatalf("create member: %v", err)
	}
	resp, body := c.Do(ctx, http.MethodPost, "/v1/auth/login", "", map[string]string{"email": email, "password": "correct horse battery"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member login = %d %s", resp.StatusCode, body)
	}
	return decode[sessionBody](t, body).Data.AccessToken
}

func TestAPIKeysHTTP(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := apitest.New(t, env)
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	owner, orgID := registerOwner(ctx, t, c, "owner@acme.io", "Acme")
	member := memberToken(ctx, t, c, pool, orgID, "member@acme.io")

	resp, body := c.Do(ctx, http.MethodPost, "/v1/api-keys", owner, map[string]string{"name": "ci"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %s", resp.StatusCode, body)
	}
	created := decode[struct {
		Data struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Prefix string `json:"prefix"`
			Key    string `json:"key"`
		} `json:"data"`
	}](t, body)
	if !strings.HasPrefix(created.Data.Key, "whk_") || !strings.HasPrefix(created.Data.Key, created.Data.Prefix) || created.Data.Name != "ci" {
		t.Fatalf("created = %s", body)
	}

	svc := service.NewAPIKeyService(postgres.NewAPIKeyRepo(pool), platform.SystemClock{})
	if got, err := svc.Authenticate(ctx, created.Data.Key); err != nil || got.String() != orgID {
		t.Fatalf("authenticate fresh key = %v, %v", got, err)
	}

	t.Run("list never exposes the key", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodGet, "/v1/api-keys?limit=10", owner, nil)
		if resp.StatusCode != http.StatusOK || strings.Contains(string(body), created.Data.Key) || strings.Contains(string(body), `"key"`) {
			t.Errorf("list = %d %s", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), `"has_more":false`) {
			t.Errorf("meta missing: %s", body)
		}
	})

	t.Run("members cannot manage keys", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{http.MethodPost, "/v1/api-keys"}, {http.MethodGet, "/v1/api-keys"}, {http.MethodDelete, "/v1/api-keys/" + created.Data.ID},
		} {
			resp, body := c.Do(ctx, tc.method, tc.path, member, map[string]string{"name": "x"})
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s as member = %d %s, want 403", tc.method, tc.path, resp.StatusCode, body)
			}
		}
	})

	t.Run("validation and pagination errors", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodPost, "/v1/api-keys", owner, map[string]string{"name": ""})
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"field":"name"`) {
			t.Errorf("empty name = %d %s", resp.StatusCode, body)
		}
		if resp, _ := c.Do(ctx, http.MethodGet, "/v1/api-keys?limit=0", owner, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=0 = %d, want 400", resp.StatusCode)
		}
		if resp, _ := c.Do(ctx, http.MethodGet, "/v1/api-keys?cursor=zzz", owner, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad cursor = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("revoke takes effect immediately", func(t *testing.T) {
		if resp, body := c.Do(ctx, http.MethodDelete, "/v1/api-keys/"+created.Data.ID, owner, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("revoke = %d %s", resp.StatusCode, body)
		}
		if _, err := svc.Authenticate(ctx, created.Data.Key); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("revoked key err = %v, want ErrUnauthorized (FR-K3)", err)
		}
		if resp, _ := c.Do(ctx, http.MethodDelete, "/v1/api-keys/"+created.Data.ID, owner, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("second revoke = %d, want 404", resp.StatusCode)
		}
	})
}

type listResp struct {
	Data []map[string]any `json:"data"`
	Meta struct {
		NextCursor string `json:"next_cursor"`
		Limit      int    `json:"limit"`
		HasMore    bool   `json:"has_more"`
	} `json:"meta"`
}

type itemResp struct {
	Data map[string]any `json:"data"`
}

func TestEventTypesHTTP(t *testing.T) {
	env := testenv.Start(t)
	ctx := t.Context()
	c := apitest.New(t, env)
	token, _ := registerOwner(ctx, t, c, "owner@acme.io", "Acme")
	otherToken, _ := registerOwner(ctx, t, c, "owner@other.io", "Other")

	resp, body := c.Do(ctx, http.MethodPost, "/v1/event-types", token, map[string]any{
		"name": "payment.paid", "description": "A payment succeeded", "example_payload": map[string]any{"amount": 10},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %s", resp.StatusCode, body)
	}
	created := decode[itemResp](t, body).Data
	id, _ := created["id"].(string)
	if created["name"] != "payment.paid" || id == "" || resp.Header.Get("Location") != "/v1/event-types/"+id {
		t.Fatalf("created = %v, location = %q", created, resp.Header.Get("Location"))
	}

	t.Run("duplicate name", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodPost, "/v1/event-types", token, map[string]any{"name": "payment.paid"})
		if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "ALREADY_EXISTS") {
			t.Errorf("duplicate = %d %s", resp.StatusCode, body)
		}
	})

	t.Run("validation", func(t *testing.T) {
		for name, req := range map[string]map[string]any{
			"uppercase name":  {"name": "Payment.Paid"},
			"example string":  {"name": "ok", "example_payload": "text"},
			"example array":   {"name": "ok", "example_payload": []int{1}},
			"missing name":    {"description": "x"},
			"unknown field":   {"name": "ok", "color": "red"},
			"name not string": {"name": 5},
		} {
			resp, body := c.Do(ctx, http.MethodPost, "/v1/event-types", token, req)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "VALIDATION_FAILED") {
				t.Errorf("%s = %d %s, want 400 VALIDATION_FAILED", name, resp.StatusCode, body)
			}
		}
	})

	t.Run("patch keeps what it does not mention and rejects renames", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodPatch, "/v1/event-types/"+id, token, map[string]any{"description": "new text"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("patch = %d %s", resp.StatusCode, body)
		}
		got := decode[itemResp](t, body).Data
		if got["description"] != "new text" || got["example_payload"] == nil {
			t.Errorf("patched = %v; example must be kept", got)
		}
		if r, _ := c.Do(ctx, http.MethodPatch, "/v1/event-types/"+id, token, map[string]any{"name": "renamed"}); r.StatusCode != http.StatusBadRequest {
			t.Errorf("rename = %d, want 400", r.StatusCode)
		}
		resp, body = c.Do(ctx, http.MethodPatch, "/v1/event-types/"+id, token, map[string]any{"example_payload": nil})
		if resp.StatusCode != http.StatusOK || decode[itemResp](t, body).Data["example_payload"] != nil {
			t.Errorf("null example must clear it: %d %s", resp.StatusCode, body)
		}
	})

	t.Run("other org sees 404", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, "/v1/event-types/" + id}, {http.MethodPatch, "/v1/event-types/" + id}, {http.MethodDelete, "/v1/event-types/" + id},
		} {
			resp, body := c.Do(ctx, tc.method, tc.path, otherToken, map[string]any{"description": "hijack"})
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s as other org = %d %s, want 404", tc.method, tc.path, resp.StatusCode, body)
			}
		}
		if resp, _ := c.Do(ctx, http.MethodGet, "/v1/event-types/"+id, token, nil); resp.StatusCode != http.StatusOK {
			t.Errorf("owner must still see it: %d", resp.StatusCode)
		}
	})

	t.Run("list paginates with cursor", func(t *testing.T) {
		for _, n := range []string{"a.one", "a.two", "a.three"} {
			if resp, body := c.Do(ctx, http.MethodPost, "/v1/event-types", token, map[string]any{"name": n}); resp.StatusCode != http.StatusCreated {
				t.Fatalf("create %s = %d %s", n, resp.StatusCode, body)
			}
		}
		resp, body := c.Do(ctx, http.MethodGet, "/v1/event-types?limit=2", token, nil)
		first := decode[listResp](t, body)
		if resp.StatusCode != http.StatusOK || len(first.Data) != 2 || !first.Meta.HasMore || first.Meta.NextCursor == "" || first.Meta.Limit != 2 {
			t.Fatalf("page1 = %d %s", resp.StatusCode, body)
		}
		_, body = c.Do(ctx, http.MethodGet, "/v1/event-types?limit=2&cursor="+first.Meta.NextCursor, token, nil)
		second := decode[listResp](t, body)
		if len(second.Data) != 2 || second.Meta.HasMore {
			t.Fatalf("page2 = %s", body)
		}
		seen := map[any]bool{}
		for _, row := range append(first.Data, second.Data...) {
			seen[row["id"]] = true
		}
		if len(seen) != 4 {
			t.Errorf("pages overlap or lose rows: %d distinct of 4", len(seen))
		}
	})

	t.Run("delete", func(t *testing.T) {
		if resp, body := c.Do(ctx, http.MethodDelete, "/v1/event-types/"+id, token, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete = %d %s", resp.StatusCode, body)
		}
		if resp, _ := c.Do(ctx, http.MethodGet, "/v1/event-types/"+id, token, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("get after delete = %d, want 404", resp.StatusCode)
		}
	})
}

func TestConsumersAndEndpointsHTTP(t *testing.T) {
	env := testenv.Start(t)
	ctx := t.Context()
	c := apitest.New(t, env)
	token, _ := registerOwner(ctx, t, c, "owner@acme.io", "Acme")

	if resp, body := c.Do(ctx, http.MethodPost, "/v1/event-types", token, map[string]any{"name": "payment.paid"}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("event type = %d %s", resp.StatusCode, body)
	}
	resp, body := c.Do(ctx, http.MethodPost, "/v1/consumers", token, map[string]any{"uid": "shop_42", "name": "Shop 42"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create consumer = %d %s", resp.StatusCode, body)
	}
	consumer := decode[itemResp](t, body).Data
	consumerID, _ := consumer["id"].(string)
	if consumer["uid"] != "shop_42" || resp.Header.Get("Location") != "/v1/consumers/"+consumerID {
		t.Fatalf("consumer = %v", consumer)
	}

	t.Run("consumer uid is unique and immutable", func(t *testing.T) {
		dup, dupBody := c.Do(ctx, http.MethodPost, "/v1/consumers", token, map[string]any{"uid": "shop_42"})
		if dup.StatusCode != http.StatusConflict {
			t.Errorf("duplicate uid = %d %s, want 409", dup.StatusCode, dupBody)
		}
		if r, _ := c.Do(ctx, http.MethodPatch, "/v1/consumers/"+consumerID, token, map[string]any{"uid": "x"}); r.StatusCode != http.StatusBadRequest {
			t.Errorf("patch uid = %d, want 400", r.StatusCode)
		}
		renamed, renamedBody := c.Do(ctx, http.MethodPatch, "/v1/consumers/"+consumerID, token, map[string]any{"name": "Renamed"})
		if renamed.StatusCode != http.StatusOK || decode[itemResp](t, renamedBody).Data["name"] != "Renamed" {
			t.Errorf("patch name = %d %s", renamed.StatusCode, renamedBody)
		}
	})

	path := "/v1/consumers/" + consumerID + "/endpoints"
	resp, body = c.Do(ctx, http.MethodPost, path, token, map[string]any{
		"url": "https://example.com/hooks", "description": "main", "event_types": []string{"payment.paid"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create endpoint = %d %s", resp.StatusCode, body)
	}
	ep := decode[itemResp](t, body).Data
	epID, _ := ep["id"].(string)
	secret, _ := ep["secret"].(string)
	if !strings.HasPrefix(secret, "whsec_") || ep["enabled"] != true || ep["consumer_id"] != consumerID {
		t.Fatalf("endpoint = %v", ep)
	}

	t.Run("secret is shown on create and rotate only", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodGet, "/v1/endpoints/"+epID, token, nil)
		if resp.StatusCode != http.StatusOK || strings.Contains(string(body), "secret") || strings.Contains(string(body), secret) {
			t.Errorf("get leaks the secret: %d %s", resp.StatusCode, body)
		}
		_, body = c.Do(ctx, http.MethodGet, path, token, nil)
		if strings.Contains(string(body), "secret") || strings.Contains(string(body), secret) {
			t.Errorf("list leaks the secret: %s", body)
		}
		resp, body = c.Do(ctx, http.MethodPost, "/v1/endpoints/"+epID+"/rotate-secret", token, nil)
		rotated, _ := decode[itemResp](t, body).Data["secret"].(string)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(rotated, "whsec_") || rotated == secret {
			t.Errorf("rotate = %d %s", resp.StatusCode, body)
		}
	})

	t.Run("validation", func(t *testing.T) {
		cases := map[string]struct {
			req  map[string]any
			code int
			want string
		}{
			"ftp url":            {map[string]any{"url": "ftp://x.io"}, http.StatusBadRequest, `"field":"url"`},
			"missing url":        {map[string]any{"description": "x"}, http.StatusBadRequest, `"field":"url"`},
			"unknown event type": {map[string]any{"url": "https://x.io", "event_types": []string{"nope"}}, http.StatusUnprocessableEntity, "nope"},
			"unknown field":      {map[string]any{"url": "https://x.io", "secret": "mine"}, http.StatusBadRequest, "VALIDATION_FAILED"},
		}
		for name, tc := range cases {
			resp, body := c.Do(ctx, http.MethodPost, path, token, tc.req)
			if resp.StatusCode != tc.code || !strings.Contains(string(body), tc.want) {
				t.Errorf("%s = %d %s, want %d containing %s", name, resp.StatusCode, body, tc.code, tc.want)
			}
		}
	})

	t.Run("disable, update and delete", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodPatch, "/v1/endpoints/"+epID, token, map[string]any{"enabled": false})
		got := decode[itemResp](t, body).Data
		if resp.StatusCode != http.StatusOK || got["enabled"] != false || got["url"] != "https://example.com/hooks" {
			t.Errorf("disable = %d %s", resp.StatusCode, body)
		}
		_, body = c.Do(ctx, http.MethodGet, "/v1/endpoints/"+epID, token, nil)
		if decode[itemResp](t, body).Data["enabled"] != false {
			t.Errorf("disabled flag must persist: %s", body)
		}
		resp, body = c.Do(ctx, http.MethodPatch, "/v1/endpoints/"+epID, token, map[string]any{"event_types": []string{}})
		if resp.StatusCode != http.StatusOK || strings.Contains(string(body), "payment.paid") {
			t.Errorf("clearing subscriptions = %d %s", resp.StatusCode, body)
		}
		if resp, _ := c.Do(ctx, http.MethodDelete, "/v1/endpoints/"+epID, token, nil); resp.StatusCode != http.StatusNoContent {
			t.Errorf("delete = %d", resp.StatusCode)
		}
		if resp, _ := c.Do(ctx, http.MethodGet, "/v1/endpoints/"+epID, token, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("get deleted = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("deleting a consumer removes its endpoints", func(t *testing.T) {
		resp, body := c.Do(ctx, http.MethodPost, path, token, map[string]any{"url": "https://example.com/other"})
		other, _ := decode[itemResp](t, body).Data["id"].(string)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create = %d %s", resp.StatusCode, body)
		}
		if resp, _ := c.Do(ctx, http.MethodDelete, "/v1/consumers/"+consumerID, token, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("delete consumer = %d", resp.StatusCode)
		}
		if resp, _ := c.Do(ctx, http.MethodGet, "/v1/endpoints/"+other, token, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("endpoint after consumer delete = %d, want 404", resp.StatusCode)
		}
		if resp, _ := c.Do(ctx, http.MethodGet, path, token, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("list endpoints of deleted consumer = %d, want 404", resp.StatusCode)
		}
	})
}

// TestCrossOrgIsolation proves FR-A3: org B can never read or change org A's data by id,
// and org A's rows are unchanged afterwards.
func TestCrossOrgIsolation(t *testing.T) {
	env := testenv.Start(t)
	ctx := t.Context()
	c := apitest.New(t, env)
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	tokenA, _ := registerOwner(ctx, t, c, "a@a.io", "A")
	tokenB, _ := registerOwner(ctx, t, c, "b@b.io", "B")

	create := func(path string, body any) string {
		resp, raw := c.Do(ctx, http.MethodPost, path, tokenA, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", path, resp.StatusCode, raw)
		}
		id, _ := decode[itemResp](t, raw).Data["id"].(string)
		return id
	}
	keyID := create("/v1/api-keys", map[string]any{"name": "k"})
	typeID := create("/v1/event-types", map[string]any{"name": "order.created"})
	consumerID := create("/v1/consumers", map[string]any{"uid": "c1", "name": "C1"})
	endpointID := create("/v1/consumers/"+consumerID+"/endpoints", map[string]any{"url": "https://a.example.com/hook"})

	var secretBefore []byte
	if err := pool.QueryRow(ctx, `SELECT secret_ciphertext FROM endpoints WHERE id = $1`, endpointID).Scan(&secretBefore); err != nil {
		t.Fatalf("read secret: %v", err)
	}

	attacks := []struct {
		method, path string
		body         any
	}{
		{http.MethodDelete, "/v1/api-keys/" + keyID, nil},
		{http.MethodGet, "/v1/event-types/" + typeID, nil},
		{http.MethodPatch, "/v1/event-types/" + typeID, map[string]any{"description": "hijacked"}},
		{http.MethodDelete, "/v1/event-types/" + typeID, nil},
		{http.MethodGet, "/v1/consumers/" + consumerID, nil},
		{http.MethodPatch, "/v1/consumers/" + consumerID, map[string]any{"name": "hijacked"}},
		{http.MethodDelete, "/v1/consumers/" + consumerID, nil},
		{http.MethodGet, "/v1/consumers/" + consumerID + "/endpoints", nil},
		{http.MethodPost, "/v1/consumers/" + consumerID + "/endpoints", map[string]any{"url": "https://evil.example.com/hook"}},
		{http.MethodGet, "/v1/endpoints/" + endpointID, nil},
		{http.MethodPatch, "/v1/endpoints/" + endpointID, map[string]any{"enabled": false, "url": "https://evil.example.com/hook"}},
		{http.MethodPost, "/v1/endpoints/" + endpointID + "/rotate-secret", nil},
		{http.MethodDelete, "/v1/endpoints/" + endpointID, nil},
	}
	for _, a := range attacks {
		resp, body := c.Do(ctx, a.method, a.path, tokenB, a.body)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("org B %s %s = %d %s, want 404", a.method, a.path, resp.StatusCode, body)
		}
	}

	// Org B's own lists must not contain org A's rows.
	for _, path := range []string{"/v1/event-types", "/v1/consumers", "/v1/api-keys"} {
		_, body := c.Do(ctx, http.MethodGet, path, tokenB, nil)
		if got := decode[listResp](t, body); len(got.Data) != 0 {
			t.Errorf("org B GET %s sees %d rows of org A", path, len(got.Data))
		}
	}

	// Nothing of org A changed.
	var (
		desc, url string
		enabled   bool
		secret    []byte
		revoked   *time.Time
		consumers int
	)
	if err := pool.QueryRow(ctx, `SELECT description FROM event_types WHERE id = $1`, typeID).Scan(&desc); err != nil || desc != "" {
		t.Errorf("event type after attacks: desc=%q err=%v", desc, err)
	}
	if err := pool.QueryRow(ctx, `SELECT url, enabled, secret_ciphertext FROM endpoints WHERE id = $1`, endpointID).Scan(&url, &enabled, &secret); err != nil ||
		url != "https://a.example.com/hook" || !enabled || !bytes.Equal(secret, secretBefore) {
		t.Errorf("endpoint after attacks: url=%q enabled=%v secretChanged=%v err=%v", url, enabled, !bytes.Equal(secret, secretBefore), err)
	}
	if err := pool.QueryRow(ctx, `SELECT revoked_at FROM api_keys WHERE id = $1`, keyID).Scan(&revoked); err != nil || revoked != nil {
		t.Errorf("api key after attacks: revoked=%v err=%v", revoked, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM consumers WHERE id = $1 AND name = 'C1'`, consumerID).Scan(&consumers); err != nil || consumers != 1 {
		t.Errorf("consumer after attacks: count=%d err=%v", consumers, err)
	}
}
