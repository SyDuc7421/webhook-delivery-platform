//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres"
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
