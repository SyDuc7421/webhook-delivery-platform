//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/app"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/cache/redis"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/messaging/kafka"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/service"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/apitest"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/test/testenv"
)

// seedCatalog creates an org with one consumer and one event type, directly through repositories.
func seedCatalog(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (orgID, consumerID, typeID uuid.UUID) {
	t.Helper()
	orgID, _ = seedOrg(ctx, t, pool, "ingest"+uuid.NewString()[:8])
	consumerID, typeID = platform.NewID(), platform.NewID()
	if err := postgres.NewConsumerRepo(pool).Create(ctx, domain.Consumer{ID: consumerID, OrgID: orgID, UID: "shop_42"}); err != nil {
		t.Fatalf("seed consumer: %v", err)
	}
	if err := postgres.NewEventTypeRepo(pool).Create(ctx, domain.EventType{ID: typeID, OrgID: orgID, Name: "payment.paid"}); err != nil {
		t.Fatalf("seed event type: %v", err)
	}
	return orgID, consumerID, typeID
}

func newEvent(orgID, consumerID, typeID uuid.UUID, key string) domain.Event {
	e := domain.Event{
		ID: platform.NewID(), OrgID: orgID, ConsumerID: consumerID, EventTypeID: typeID,
		ConsumerUID: "shop_42", Type: "payment.paid", Payload: json.RawMessage(`{"amount":10}`),
	}
	if key != "" {
		e.IdempotencyKey = &key
	}
	return e
}

func TestEventAndOutboxRepositories(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	tx := postgres.NewTransactor(pool)
	events := postgres.NewEventRepo(pool)
	outbox := postgres.NewOutboxRepo(pool)
	orgID, consumerID, typeID := seedCatalog(ctx, t, pool)

	accept := func(ctx context.Context, e domain.Event) (bool, error) {
		var created bool
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			var ierr error
			created, ierr = events.Insert(ctx, e)
			if ierr != nil || !created {
				return ierr
			}
			return outbox.Enqueue(ctx, e.ID)
		})
		return created, err
	}

	t.Run("concurrent duplicate idempotency keys create exactly one event and one outbox row", func(t *testing.T) {
		var created atomic.Int32
		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := accept(ctx, newEvent(orgID, consumerID, typeID, "order-1-paid"))
				if err != nil {
					t.Errorf("accept: %v", err)
				}
				if ok {
					created.Add(1)
				}
			}()
		}
		wg.Wait()
		if created.Load() != 1 {
			t.Errorf("created = %d, want exactly 1", created.Load())
		}
		var evs, obs int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE org_id = $1 AND idempotency_key = 'order-1-paid'`, orgID).Scan(&evs); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&obs); err != nil {
			t.Fatal(err)
		}
		if evs != 1 || obs != 1 {
			t.Errorf("events = %d, outbox = %d, want 1 and 1", evs, obs)
		}
		got, err := events.GetByIdempotencyKey(ctx, orgID, "order-1-paid")
		if err != nil || got.Type != "payment.paid" || string(got.Payload) != `{"amount": 10}` {
			t.Errorf("get by key = %+v, %v", got, err)
		}
		if _, err := events.GetByIdempotencyKey(ctx, uuid.New(), "order-1-paid"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("same key in another org err = %v, want ErrNotFound", err)
		}
	})

	t.Run("events without a key never conflict", func(t *testing.T) {
		for range 3 {
			if ok, err := accept(ctx, newEvent(orgID, consumerID, typeID, "")); err != nil || !ok {
				t.Fatalf("accept = %v, %v", ok, err)
			}
		}
	})

	t.Run("lock batch hands out disjoint rows under SKIP LOCKED", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM outbox`); err != nil {
			t.Fatal(err)
		}
		for range 15 {
			if ok, err := accept(ctx, newEvent(orgID, consumerID, typeID, "")); err != nil || !ok {
				t.Fatalf("accept = %v, %v", ok, err)
			}
		}
		var first, second []domain.OutboxRecord
		hold := make(chan struct{})
		locked := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- tx.WithinTx(ctx, func(ctx context.Context) error {
				var lerr error
				first, lerr = outbox.LockBatch(ctx, 10)
				close(locked)
				<-hold // keep the row locks while the second relay runs
				return lerr
			})
		}()
		<-locked
		err := tx.WithinTx(ctx, func(ctx context.Context) error {
			var lerr error
			second, lerr = outbox.LockBatch(ctx, 10)
			return lerr
		})
		close(hold)
		if err != nil || <-done != nil {
			t.Fatalf("lock batches: %v", err)
		}
		if len(first) != 10 || len(second) != 5 {
			t.Fatalf("batches = %d and %d rows, want 10 and 5", len(first), len(second))
		}
		seen := map[int64]bool{}
		for _, r := range append(first, second...) {
			if seen[r.OutboxID] {
				t.Fatalf("outbox row %d handed to both relays", r.OutboxID)
			}
			seen[r.OutboxID] = true
		}
	})

	t.Run("lock batch orders by id and carries the event", func(t *testing.T) {
		var batch []domain.OutboxRecord
		if err := tx.WithinTx(ctx, func(ctx context.Context) error {
			var lerr error
			batch, lerr = outbox.LockBatch(ctx, 100)
			return lerr
		}); err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(batch); i++ {
			if batch[i-1].OutboxID >= batch[i].OutboxID {
				t.Fatalf("not ordered by outbox id at %d", i)
			}
		}
		if len(batch) == 0 || batch[0].Event.ConsumerUID != "shop_42" || batch[0].Event.OrgID != orgID {
			t.Errorf("record = %+v", batch[0])
		}
	})

	t.Run("deleting an event type that has events is a conflict, not a 404", func(t *testing.T) {
		err := postgres.NewEventTypeRepo(pool).Delete(ctx, orgID, typeID)
		if !errors.Is(err, domain.ErrConflict) {
			t.Errorf("err = %v, want ErrConflict", err)
		}
		if _, gerr := postgres.NewEventTypeRepo(pool).Get(ctx, orgID, typeID); gerr != nil {
			t.Errorf("the event type must survive: %v", gerr)
		}
	})

	t.Run("lock batch outside a transaction is an error", func(t *testing.T) {
		if _, err := outbox.LockBatch(ctx, 10); err == nil {
			t.Error("row locks without a transaction are meaningless and must be refused")
		}
	})

	t.Run("delete and backlog", func(t *testing.T) {
		var ids []int64
		if err := tx.WithinTx(ctx, func(ctx context.Context) error {
			batch, lerr := outbox.LockBatch(ctx, 4)
			for _, r := range batch {
				ids = append(ids, r.OutboxID)
			}
			if lerr != nil {
				return lerr
			}
			return outbox.Delete(ctx, ids)
		}); err != nil {
			t.Fatal(err)
		}
		n, err := outbox.Backlog(ctx)
		if err != nil || n != 11 {
			t.Errorf("backlog = %d, %v, want 11 (15 - 4)", n, err)
		}
	})
}

func TestRateLimiter(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := redis.NewClient(env.Config.Redis)
	defer func() { _ = client.Close() }()
	orgA, orgB := platform.NewID(), platform.NewID()

	t.Run("allows up to the limit then denies with a retry hint", func(t *testing.T) {
		rl := redis.NewRateLimiter(client, 3, time.Minute)
		for i := range 3 {
			ok, _, err := rl.Allow(ctx, orgA)
			if err != nil || !ok {
				t.Fatalf("request %d: allowed=%v err=%v", i+1, ok, err)
			}
		}
		ok, retryAfter, err := rl.Allow(ctx, orgA)
		if err != nil || ok {
			t.Fatalf("4th request: allowed=%v err=%v, want denied", ok, err)
		}
		if retryAfter <= 0 || retryAfter > time.Minute {
			t.Errorf("retryAfter = %v, want within (0, 1m]", retryAfter)
		}
		if ttl, err := client.PTTL(ctx, redis.OrgRateLimitKey(orgA.String())).Result(); err != nil || ttl <= 0 {
			t.Errorf("every key needs a TTL: pttl = %v, err = %v", ttl, err)
		}
	})

	t.Run("orgs are independent", func(t *testing.T) {
		rl := redis.NewRateLimiter(client, 3, time.Minute)
		if ok, _, err := rl.Allow(ctx, orgB); err != nil || !ok {
			t.Errorf("org B allowed=%v err=%v", ok, err)
		}
	})

	t.Run("the window resets", func(t *testing.T) {
		rl := redis.NewRateLimiter(client, 1, 300*time.Millisecond)
		org := platform.NewID()
		if ok, _, _ := rl.Allow(ctx, org); !ok {
			t.Fatal("first request must pass")
		}
		if ok, _, _ := rl.Allow(ctx, org); ok {
			t.Fatal("second request in the window must be denied")
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if ok, _, _ := rl.Allow(ctx, org); ok {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Error("window never reset")
	})

	t.Run("concurrent requests never exceed the limit", func(t *testing.T) {
		rl := redis.NewRateLimiter(client, 10, time.Minute)
		org := platform.NewID()
		var allowed atomic.Int32
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if ok, _, err := rl.Allow(ctx, org); err == nil && ok {
					allowed.Add(1)
				}
			}()
		}
		wg.Wait()
		if allowed.Load() != 10 {
			t.Errorf("allowed = %d, want exactly 10", allowed.Load())
		}
	})

	t.Run("redis down is an error the caller can fail open on", func(t *testing.T) {
		rl := redis.NewRateLimiter(client, 3, time.Minute)
		timeout := 5 * time.Second
		if err := env.Redis.Stop(ctx, &timeout); err != nil {
			t.Fatalf("stop redis: %v", err)
		}
		if _, _, err := rl.Allow(ctx, orgA); !errors.Is(err, domain.ErrUnavailable) {
			t.Errorf("err = %v, want ErrUnavailable", err)
		}
	})
}

type ingestResp struct {
	Data struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"data"`
}

func TestIngest(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := apitest.New(t, env)
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	owner, orgID := registerOwner(ctx, t, c, "owner@acme.io", "Acme")
	mustCreate := func(path string, body any) []byte {
		t.Helper()
		resp, raw := c.Do(ctx, http.MethodPost, path, owner, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", path, resp.StatusCode, raw)
		}
		return raw
	}
	mustCreate("/v1/event-types", map[string]any{"name": "payment.paid"})
	mustCreate("/v1/consumers", map[string]any{"uid": "shop_42"})
	keyBody := decode[struct {
		Data struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"data"`
	}](t, mustCreate("/v1/api-keys", map[string]any{"name": "ingest"}))
	key := keyBody.Data.Key

	counts := func() (events, outbox int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE org_id = $1`, orgID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		return events, outbox
	}
	send := func(token string, headers map[string]string, body string) (apitest.Response, []byte) {
		t.Helper()
		return c.DoRaw(ctx, http.MethodPost, "/v1/events", token, strings.NewReader(body), headers)
	}
	event := func(payload string) string {
		return `{"consumer_uid":"shop_42","type":"payment.paid","payload":` + payload + `}`
	}

	t.Run("accepted without Kafka: event and outbox row are committed together", func(t *testing.T) {
		resp, body := send(key, map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}, event(`{"amount":10}`))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("ingest = %d %s", resp.StatusCode, body)
		}
		got := decode[ingestResp](t, body)
		if got.Data.Status != "accepted" || got.Data.ID == "" {
			t.Fatalf("body = %s", body)
		}
		if e, o := counts(); e != 1 || o != 1 {
			t.Errorf("events = %d, outbox = %d, want 1 and 1 (FR-I4: no relay is running)", e, o)
		}
		var tp *string
		if err := pool.QueryRow(ctx, `SELECT traceparent FROM events WHERE id = $1`, got.Data.ID).Scan(&tp); err != nil || tp == nil || !strings.HasPrefix(*tp, "00-0af765") {
			t.Errorf("traceparent = %v, err = %v; the trace context must be stored for the relay", tp, err)
		}
	})

	t.Run("authentication", func(t *testing.T) {
		for name, token := range map[string]string{"no key": "", "garbage": "nope", "unknown key": "whk_" + strings.Repeat("A", 43), "dashboard jwt": owner} {
			resp, body := send(token, nil, event(`{}`))
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s = %d %s, want 401", name, resp.StatusCode, body)
			}
		}
	})

	t.Run("payload boundaries", func(t *testing.T) {
		exact := `{"k":"` + strings.Repeat("a", 262144-8) + `"}`
		over := `{"k":"` + strings.Repeat("a", 262144-7) + `"}`
		if resp, body := send(key, nil, event(exact)); resp.StatusCode != http.StatusAccepted {
			t.Errorf("exactly 262144 bytes = %d %.200s, want 202", resp.StatusCode, body)
		}
		if resp, body := send(key, nil, event(over)); resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(body), "PAYLOAD_TOO_LARGE") {
			t.Errorf("262145 bytes = %d %.200s, want 413", resp.StatusCode, body)
		}
		huge := event(`{"k":"` + strings.Repeat("a", 400000) + `"}`)
		if resp, _ := send(key, nil, huge); resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("400 KB body = %d, want 413", resp.StatusCode)
		}
	})

	t.Run("malformed requests are 400", func(t *testing.T) {
		for name, body := range map[string]string{
			"trailing garbage": event(`{}`) + ` junk`,
			"second object":    event(`{}`) + `{}`,
			"unknown field":    `{"consumer_uid":"shop_42","type":"payment.paid","payload":{},"priority":1}`,
			"payload array":    event(`[1,2]`),
			"payload string":   event(`"x"`),
			"payload null":     event(`null`),
			"missing payload":  `{"consumer_uid":"shop_42","type":"payment.paid"}`,
			"missing type":     `{"consumer_uid":"shop_42","payload":{}}`,
			"not json":         `hello`,
			"empty body":       ``,
		} {
			resp, raw := send(key, nil, body)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "VALIDATION_FAILED") {
				t.Errorf("%s = %d %.200s, want 400 VALIDATION_FAILED", name, resp.StatusCode, raw)
			}
		}
	})

	t.Run("unknown consumer or event type is 422", func(t *testing.T) {
		resp, body := send(key, nil, `{"consumer_uid":"nobody","type":"payment.paid","payload":{}}`)
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "consumer_uid") {
			t.Errorf("unknown consumer = %d %s", resp.StatusCode, body)
		}
		resp, body = send(key, nil, `{"consumer_uid":"shop_42","type":"nope.nope","payload":{}}`)
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), `"field":"type"`) {
			t.Errorf("unknown type = %d %s", resp.StatusCode, body)
		}
	})

	t.Run("concurrent requests with one Idempotency-Key create one event", func(t *testing.T) {
		beforeE, beforeO := counts()
		ids := make(chan string, 10)
		var replayed atomic.Int32
		var wg sync.WaitGroup
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, body := send(key, map[string]string{"Idempotency-Key": "order-1-paid"}, event(`{"n":1}`))
				if resp.StatusCode != http.StatusAccepted {
					t.Errorf("status = %d %s", resp.StatusCode, body)
					return
				}
				if resp.Header.Get("Idempotent-Replayed") == "true" {
					replayed.Add(1)
				}
				ids <- decode[ingestResp](t, body).Data.ID
			}()
		}
		wg.Wait()
		close(ids)
		distinct := map[string]bool{}
		for id := range ids {
			distinct[id] = true
		}
		if len(distinct) != 1 {
			t.Errorf("distinct ids = %d, want 1: %v", len(distinct), distinct)
		}
		if replayed.Load() != 9 {
			t.Errorf("replayed responses = %d, want 9", replayed.Load())
		}
		if e, o := counts(); e-beforeE != 1 || o-beforeO != 1 {
			t.Errorf("new events = %d, new outbox rows = %d, want 1 and 1", e-beforeE, o-beforeO)
		}
		resp, _ := send(key, map[string]string{"Idempotency-Key": "bad key with spaces"}, event(`{}`))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("invalid Idempotency-Key = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("a revoked key is rejected immediately", func(t *testing.T) {
		if resp, body := c.Do(ctx, http.MethodDelete, "/v1/api-keys/"+keyBody.Data.ID, owner, nil); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("revoke = %d %s", resp.StatusCode, body)
		}
		if resp, body := send(key, nil, event(`{}`)); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("revoked key = %d %s, want 401 (FR-K3)", resp.StatusCode, body)
		}
	})

	t.Run("redis down does not stop ingest", func(t *testing.T) {
		fresh := decode[struct {
			Data struct {
				Key string `json:"key"`
			} `json:"data"`
		}](t, mustCreate("/v1/api-keys", map[string]any{"name": "fresh"})).Data.Key
		timeout := 5 * time.Second
		if err := env.Redis.Stop(ctx, &timeout); err != nil {
			t.Fatalf("stop redis: %v", err)
		}
		beforeE, _ := counts()
		resp, body := send(fresh, nil, event(`{"redis":"down"}`))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("ingest with redis down = %d %s, want 202 (NFR-D1)", resp.StatusCode, body)
		}
		if e, _ := counts(); e != beforeE+1 {
			t.Errorf("event not stored while redis was down")
		}
	})
}

func TestIngestRateLimit(t *testing.T) {
	env := testenv.Start(t)
	env.Config.Ingest.RateLimitPerMin = 3
	ctx := t.Context()
	c := apitest.New(t, env)
	owner, _ := registerOwner(ctx, t, c, "owner@acme.io", "Acme")
	for path, body := range map[string]any{
		"/v1/event-types": map[string]any{"name": "payment.paid"}, "/v1/consumers": map[string]any{"uid": "shop_42"},
	} {
		if resp, raw := c.Do(ctx, http.MethodPost, path, owner, body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", path, resp.StatusCode, raw)
		}
	}
	_, raw := c.Do(ctx, http.MethodPost, "/v1/api-keys", owner, map[string]any{"name": "k"})
	key := decode[struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}](t, raw).Data.Key

	send := func() (apitest.Response, []byte) {
		return c.Do(ctx, http.MethodPost, "/v1/events", key, map[string]any{"consumer_uid": "shop_42", "type": "payment.paid", "payload": map[string]any{}})
	}
	for i := range 3 {
		if resp, body := send(); resp.StatusCode != http.StatusAccepted {
			t.Fatalf("request %d = %d %s", i+1, resp.StatusCode, body)
		}
	}
	resp, body := send()
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "RATE_LIMITED") {
		t.Fatalf("4th request = %d %s, want 429", resp.StatusCode, body)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" || ra == "0" {
		t.Errorf("Retry-After = %q, want a positive number of seconds (FR-I5)", ra)
	}
}

func TestIngestToKafka(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	c := apitest.New(t, env)
	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	owner, orgID := registerOwner(ctx, t, c, "owner@acme.io", "Acme")
	for path, body := range map[string]any{
		"/v1/event-types": map[string]any{"name": "payment.paid"}, "/v1/consumers": map[string]any{"uid": "shop_42"},
	} {
		if resp, raw := c.Do(ctx, http.MethodPost, path, owner, body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", path, resp.StatusCode, raw)
		}
	}
	_, raw := c.Do(ctx, http.MethodPost, "/v1/api-keys", owner, map[string]any{"name": "k"})
	key := decode[struct {
		Data struct {
			Key string `json:"key"`
		} `json:"data"`
	}](t, raw).Data.Key

	ingest := func(traceparent string) string {
		t.Helper()
		headers := map[string]string{}
		if traceparent != "" {
			headers["traceparent"] = traceparent
		}
		resp, body := c.DoRaw(ctx, http.MethodPost, "/v1/events", key,
			strings.NewReader(`{"consumer_uid":"shop_42","type":"payment.paid","payload":{"n":1}}`), headers)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("ingest = %d %s", resp.StatusCode, body)
		}
		return decode[ingestResp](t, body).Data.ID
	}
	outboxRows := func() int {
		t.Helper()
		var n int
		if qerr := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&n); qerr != nil {
			t.Fatal(qerr)
		}
		return n
	}

	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	wantIDs := map[string]bool{ingest(tp): true, ingest(""): true, ingest(""): true}
	if outboxRows() != 3 {
		t.Fatalf("outbox rows = %d, want 3 while the relay is stopped", outboxRows())
	}

	prefix := "t" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + "."
	admin, err := kafka.NewClient(env.Config.Kafka)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	defer admin.Close()
	if _, cerr := kadm.NewClient(admin).CreateTopics(ctx, 1, 1, nil, kafka.Name(prefix, kafka.TopicEvents)); cerr != nil {
		t.Fatalf("create topic: %v", cerr)
	}

	producerClient, err := kafka.NewClient(env.Config.Kafka, kgo.UnknownTopicRetries(0))
	if err != nil {
		t.Fatalf("producer client: %v", err)
	}
	defer producerClient.Close()
	newRelay := func(topicPrefix string) *service.Relay {
		return service.NewRelay(postgres.NewTransactor(pool), postgres.NewOutboxRepo(pool),
			kafka.NewEventProducer(producerClient, topicPrefix), 100,
			prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_backlog"}))
	}

	t.Run("relay moves every outbox row to the events topic", func(t *testing.T) {
		moved, err := newRelay(prefix).RunOnce(ctx)
		if err != nil || moved != 3 {
			t.Fatalf("RunOnce = %d, %v", moved, err)
		}
		if n := outboxRows(); n != 0 {
			t.Errorf("outbox rows after the relay = %d, want 0", n)
		}

		consumer, err := kafka.NewClient(env.Config.Kafka,
			kgo.ConsumeTopics(kafka.Name(prefix, kafka.TopicEvents)), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
		if err != nil {
			t.Fatalf("consumer: %v", err)
		}
		defer consumer.Close()
		pollCtx, pollCancel := context.WithTimeout(ctx, 40*time.Second)
		defer pollCancel()
		var got []*kgo.Record
		for len(got) < 3 && pollCtx.Err() == nil {
			fetches := consumer.PollFetches(pollCtx)
			fetches.EachRecord(func(r *kgo.Record) { got = append(got, r) })
		}
		if len(got) != 3 {
			t.Fatalf("consumed %d records, want 3", len(got))
		}
		sawTrace := false
		for _, r := range got {
			if string(r.Key) != "shop_42" {
				t.Errorf("key = %q, want the consumer uid (ordering key)", r.Key)
			}
			headers := map[string]string{}
			for _, h := range r.Headers {
				headers[h.Key] = string(h.Value)
			}
			if headers["schema-version"] != "1" {
				t.Errorf("headers = %v, want schema-version=1", headers)
			}
			if headers["traceparent"] == tp {
				sawTrace = true
			}
			var m map[string]any
			if err := json.Unmarshal(r.Value, &m); err != nil {
				t.Fatalf("value is not JSON: %v", err)
			}
			if !wantIDs[m["event_id"].(string)] || m["org_id"] != orgID || m["consumer_uid"] != "shop_42" || m["type"] != "payment.paid" || m["accepted_at"] == nil {
				t.Errorf("value = %s", r.Value)
			}
			if _, leaked := m["payload"]; leaked {
				t.Error("records carry ids, not payloads (docs/MESSAGING.md)")
			}
		}
		if !sawTrace {
			t.Error("the traceparent given at ingest must reach the record headers (NFR-O1)")
		}
	})

	t.Run("a failed publish leaves the rows in the outbox", func(t *testing.T) {
		ingest("")
		missing := "m" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + "." // topic never created
		moved, err := newRelay(missing).RunOnce(ctx)
		if err == nil || moved != 0 {
			t.Fatalf("RunOnce = %d, %v; publishing to a missing topic must fail", moved, err)
		}
		if n := outboxRows(); n != 1 {
			t.Errorf("outbox rows = %d, want 1: nothing may be dropped when Kafka rejects the publish", n)
		}
		moved, err = newRelay(prefix).RunOnce(ctx)
		if err != nil || moved != 1 || outboxRows() != 0 {
			t.Errorf("retry with a good topic = %d, %v, rows %d; the relay must recover", moved, err, outboxRows())
		}
	})
}

func TestRelayComponentDrainsTheOutbox(t *testing.T) {
	env := testenv.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	prefix := "r" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + "."
	env.Config.Kafka.TopicPrefix = prefix
	env.Config.Outbox.BatchSize = 2
	env.Config.Outbox.PollInterval = 50 * time.Millisecond

	pool, err := postgres.NewPool(ctx, env.Config.Postgres)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	orgID, consumerID, typeID := seedCatalog(ctx, t, pool)
	tx, events, outbox := postgres.NewTransactor(pool), postgres.NewEventRepo(pool), postgres.NewOutboxRepo(pool)
	for range 5 { // 5 events with batch size 2: the component must drain across several batches
		e := newEvent(orgID, consumerID, typeID, "")
		if aerr := tx.WithinTx(ctx, func(ctx context.Context) error {
			if _, ierr := events.Insert(ctx, e); ierr != nil {
				return ierr
			}
			return outbox.Enqueue(ctx, e.ID)
		}); aerr != nil {
			t.Fatalf("accept: %v", aerr)
		}
	}

	admin, err := kafka.NewClient(env.Config.Kafka)
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	defer admin.Close()
	if _, cerr := kadm.NewClient(admin).CreateTopics(ctx, 1, 1, nil, kafka.Name(prefix, kafka.TopicEvents)); cerr != nil {
		t.Fatalf("create topic: %v", cerr)
	}
	kc, err := kafka.NewClient(env.Config.Kafka)
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}
	defer kc.Close()

	reg := prometheus.NewRegistry()
	comp, err := app.NewRelayComponent(app.RelayDeps{
		Config: env.Config, Pool: pool, Kafka: kc, Registry: reg,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("relay component: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- comp.Start(ctx) }()

	deadline := time.Now().Add(40 * time.Second)
	for {
		n, berr := outbox.Backlog(ctx)
		if berr != nil {
			t.Fatal(berr)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox still has %d rows after 40s", n)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if serr := comp.Stop(ctx); serr != nil {
		t.Fatalf("stop: %v", serr)
	}
	if derr := <-done; derr != nil {
		t.Fatalf("start returned %v", derr)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range families {
		if f.GetName() == "webhook_outbox_backlog" {
			found = true
			if v := f.GetMetric()[0].GetGauge().GetValue(); v != 0 {
				t.Errorf("webhook_outbox_backlog = %v, want 0 after draining", v)
			}
		}
	}
	if !found {
		t.Error("webhook_outbox_backlog must be registered (NFR-O2)")
	}
}
