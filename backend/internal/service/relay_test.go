package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type fakePublisher struct {
	mu        sync.Mutex
	published [][]domain.Event
	err       error
}

func (f *fakePublisher) PublishEvents(_ context.Context, events []domain.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, events)
	return nil
}

func records(n int) []domain.OutboxRecord {
	out := make([]domain.OutboxRecord, 0, n)
	for i := range n {
		out = append(out, domain.OutboxRecord{OutboxID: int64(i + 1), Event: domain.Event{ID: uuid.New(), ConsumerUID: "shop_42"}})
	}
	return out
}

func newRelay(t *testing.T, batch []domain.OutboxRecord, pubErr error) (*Relay, *fakeOutbox, *fakePublisher, prometheus.Gauge) {
	t.Helper()
	ob := &fakeOutbox{batch: batch, queued: make([]uuid.UUID, len(batch))}
	pub := &fakePublisher{err: pubErr}
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_outbox_backlog"})
	return NewRelay(fakeTx{}, ob, pub, 10, gauge), ob, pub, gauge
}

func TestRelayPublishesThenDeletes(t *testing.T) {
	t.Parallel()
	batch := records(3)
	r, ob, pub, gauge := newRelay(t, batch, nil)

	moved, err := r.RunOnce(context.Background())
	if err != nil || moved != 3 {
		t.Fatalf("RunOnce = %d, %v", moved, err)
	}
	if len(pub.published) != 1 || len(pub.published[0]) != 3 {
		t.Fatalf("published = %v", pub.published)
	}
	for i, e := range pub.published[0] {
		if e.ID != batch[i].Event.ID {
			t.Errorf("event %d out of order or wrong", i)
		}
	}
	if !slices.Equal(ob.deleted, []int64{1, 2, 3}) {
		t.Errorf("deleted = %v, want exactly the locked ids", ob.deleted)
	}
	if got := testutil.ToFloat64(gauge); got != 3 {
		t.Errorf("backlog gauge = %v, want the fake's backlog (3)", got)
	}
}

func TestRelayPublishFailureKeepsRows(t *testing.T) {
	t.Parallel()
	boom := errors.New("kafka unreachable")
	r, ob, _, gauge := newRelay(t, records(3), boom)

	moved, err := r.RunOnce(context.Background())
	if !errors.Is(err, boom) || moved != 0 {
		t.Fatalf("RunOnce = %d, %v, want 0 and the publish error", moved, err)
	}
	if got := testutil.ToFloat64(gauge); got != 3 {
		t.Errorf("backlog gauge = %v after a failed publish, want 3: a Kafka outage is exactly when the backlog must be visible", got)
	}
	if len(ob.deleted) != 0 {
		t.Errorf("deleted = %v; rows must stay when the publish failed (ADR-0002)", ob.deleted)
	}
}

func TestRelayEmptyBatchPublishesNothing(t *testing.T) {
	t.Parallel()
	r, ob, pub, _ := newRelay(t, nil, nil)
	moved, err := r.RunOnce(context.Background())
	if err != nil || moved != 0 || len(pub.published) != 0 || len(ob.deleted) != 0 {
		t.Errorf("RunOnce = %d, %v; published=%d deleted=%d", moved, err, len(pub.published), len(ob.deleted))
	}
}

func TestRelayRespectsBatchSize(t *testing.T) {
	t.Parallel()
	r, ob, _, _ := newRelay(t, records(25), nil) // fake returns at most `limit` rows
	moved, err := r.RunOnce(context.Background())
	if err != nil || moved != 10 || len(ob.deleted) != 10 {
		t.Errorf("RunOnce = %d, %v; deleted=%d, want one batch of 10", moved, err, len(ob.deleted))
	}
}
