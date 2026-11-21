package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

const testMaxPayload = 1024

type ingestFixture struct {
	svc       *IngestService
	events    *fakeEvents
	outbox    *fakeOutbox
	consumers *fakeConsumers
	types     *fakeEventTypes
	counter   *prometheus.CounterVec
	org       uuid.UUID
}

func newIngestFixture(t *testing.T) ingestFixture {
	t.Helper()
	f := ingestFixture{
		events: newFakeEvents(), outbox: &fakeOutbox{}, consumers: newFakeConsumers(), types: newFakeEventTypes(),
		counter: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_ingested_total"}, []string{"result"}),
		org:     uuid.New(),
	}
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	f.svc = NewIngestService(fakeTx{}, f.events, f.outbox, f.consumers, f.types, testMaxPayload, clock, f.counter)
	ctx := t.Context()
	if err := f.consumers.Create(ctx, domain.Consumer{ID: uuid.New(), OrgID: f.org, UID: "shop_42"}); err != nil {
		t.Fatal(err)
	}
	if err := f.types.Create(ctx, domain.EventType{ID: uuid.New(), OrgID: f.org, Name: "payment.paid"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f ingestFixture) input(payload string) IngestInput {
	return IngestInput{OrgID: f.org, ConsumerUID: "shop_42", Type: "payment.paid", Payload: json.RawMessage(payload)}
}

func (f ingestFixture) count(result string) float64 {
	return testutil.ToFloat64(f.counter.WithLabelValues(result))
}

func TestIngestAccepts(t *testing.T) {
	t.Parallel()
	f := newIngestFixture(t)
	in := f.input(`{"amount":10}`)
	in.Traceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	res, err := f.svc.Ingest(context.Background(), in)
	if err != nil || res.Duplicate || res.EventID == uuid.Nil {
		t.Fatalf("ingest = %+v, %v", res, err)
	}
	if len(f.events.rows) != 1 || len(f.outbox.queued) != 1 || f.outbox.queued[0] != res.EventID {
		t.Fatalf("events = %d, outbox = %v; want one event and one outbox row for it", len(f.events.rows), f.outbox.queued)
	}
	e := f.events.rows[0]
	if e.ConsumerUID != "shop_42" || e.Type != "payment.paid" || e.Traceparent != in.Traceparent || string(e.Payload) != `{"amount":10}` {
		t.Errorf("stored event = %+v", e)
	}
	if f.count("accepted") != 1 {
		t.Errorf("accepted counter = %v", f.count("accepted"))
	}
}

func TestIngestIdempotency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newIngestFixture(t)
	in := f.input(`{"a":1}`)
	in.IdempotencyKey = "order-1-paid"
	first, err := f.svc.Ingest(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	in.Payload = json.RawMessage(`{"a":2}`) // a replay may differ; the original wins (FR-I3)
	second, err := f.svc.Ingest(ctx, in)
	if err != nil || !second.Duplicate || second.EventID != first.EventID {
		t.Fatalf("replay = %+v, %v; want the original id and Duplicate", second, err)
	}
	if len(f.events.rows) != 1 || len(f.outbox.queued) != 1 {
		t.Errorf("events = %d, outbox = %d; a replay must create nothing", len(f.events.rows), len(f.outbox.queued))
	}
	if f.count("accepted") != 1 || f.count("duplicate") != 1 {
		t.Errorf("counters accepted=%v duplicate=%v", f.count("accepted"), f.count("duplicate"))
	}

	t.Run("replay still returns the original after the consumer is deleted", func(t *testing.T) {
		if err := f.consumers.Delete(ctx, f.org, f.consumers.rows[0].ID); err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.Ingest(ctx, in)
		if err != nil || got.EventID != first.EventID {
			t.Errorf("replay = %+v, %v", got, err)
		}
	})

	t.Run("keys are scoped per org", func(t *testing.T) {
		g := newIngestFixture(t)
		g.org = uuid.New()
		_ = g.consumers.Create(ctx, domain.Consumer{ID: uuid.New(), OrgID: g.org, UID: "shop_42"})
		_ = g.types.Create(ctx, domain.EventType{ID: uuid.New(), OrgID: g.org, Name: "payment.paid"})
		a, err1 := g.svc.Ingest(ctx, IngestInput{OrgID: g.org, ConsumerUID: "shop_42", Type: "payment.paid", Payload: json.RawMessage(`{}`), IdempotencyKey: "k"})
		other := uuid.New()
		_ = g.consumers.Create(ctx, domain.Consumer{ID: uuid.New(), OrgID: other, UID: "shop_42"})
		_ = g.types.Create(ctx, domain.EventType{ID: uuid.New(), OrgID: other, Name: "payment.paid"})
		b, err2 := g.svc.Ingest(ctx, IngestInput{OrgID: other, ConsumerUID: "shop_42", Type: "payment.paid", Payload: json.RawMessage(`{}`), IdempotencyKey: "k"})
		if err1 != nil || err2 != nil || a.EventID == b.EventID || b.Duplicate {
			t.Errorf("a=%+v b=%+v errs=%v %v; the same key in another org is a new event", a, b, err1, err2)
		}
	})
}

func TestIngestPayloadValidation(t *testing.T) {
	t.Parallel()
	f := newIngestFixture(t)
	exact := `{"k":"` + strings.Repeat("a", testMaxPayload-8) + `"}`
	if len(exact) != testMaxPayload {
		t.Fatalf("fixture is %d bytes", len(exact))
	}
	if _, err := f.svc.Ingest(context.Background(), f.input(exact)); err != nil {
		t.Errorf("a payload of exactly the limit must be accepted: %v", err)
	}

	over := `{"k":"` + strings.Repeat("a", testMaxPayload-7) + `"}`
	if _, err := f.svc.Ingest(context.Background(), f.input(over)); !errors.Is(err, domain.ErrPayloadTooBig) {
		t.Errorf("limit+1 err = %v, want ErrPayloadTooBig", err)
	}

	for name, payload := range map[string]string{
		"empty": ``, "null": `null`, "array": `[1]`, "string": `"x"`, "number": `1`, "bool": `true`, "malformed": `{bad`,
	} {
		_, err := f.svc.Ingest(context.Background(), f.input(payload))
		if _, ok := fieldsOf(t, err)["payload"]; !ok {
			t.Errorf("%s: err = %v, want field payload", name, err)
		}
	}
	if len(f.events.rows) != 1 {
		t.Errorf("rejected payloads must not create events, have %d", len(f.events.rows))
	}
	if f.count("rejected") != 8 {
		t.Errorf("rejected counter = %v, want 8", f.count("rejected"))
	}
}

func TestIngestReferenceValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newIngestFixture(t)

	in := f.input(`{}`)
	in.ConsumerUID = "nobody"
	_, err := f.svc.Ingest(ctx, in)
	if !errors.Is(err, domain.ErrUnprocessable) {
		t.Fatalf("unknown consumer err = %v, want ErrUnprocessable", err)
	}
	if _, ok := fieldsOf(t, err)["consumer_uid"]; !ok {
		t.Errorf("want field consumer_uid, got %v", err)
	}

	in = f.input(`{}`)
	in.Type = "nope.nope"
	_, err = f.svc.Ingest(ctx, in)
	if !errors.Is(err, domain.ErrUnprocessable) {
		t.Fatalf("unknown type err = %v, want ErrUnprocessable", err)
	}
	if _, ok := fieldsOf(t, err)["type"]; !ok {
		t.Errorf("want field type, got %v", err)
	}

	in = f.input(`{}`)
	in.OrgID = uuid.New() // another org cannot use this org's consumer or event types
	if _, err := f.svc.Ingest(ctx, in); !errors.Is(err, domain.ErrUnprocessable) {
		t.Errorf("cross-org err = %v, want ErrUnprocessable", err)
	}
	if len(f.events.rows) != 0 || len(f.outbox.queued) != 0 {
		t.Error("nothing may be stored for rejected input")
	}
}

func TestIngestIdempotencyKeyValidation(t *testing.T) {
	t.Parallel()
	f := newIngestFixture(t)
	for name, key := range map[string]string{
		"too long": strings.Repeat("k", 256), "space": "a b", "control": "a\x01b", "non-ascii": "khóa", "tab": "a\tb",
	} {
		in := f.input(`{}`)
		in.IdempotencyKey = key
		_, err := f.svc.Ingest(context.Background(), in)
		if _, ok := fieldsOf(t, err)["idempotency_key"]; !ok {
			t.Errorf("%s: err = %v, want field idempotency_key", name, err)
		}
	}
	for _, key := range []string{"k", strings.Repeat("k", 255), "order:1/paid_v2", "A-Za-z0-9._~:/?#[]@!$&'()*+,;=%"} {
		in := f.input(`{}`)
		in.IdempotencyKey = key
		if _, err := f.svc.Ingest(context.Background(), in); err != nil {
			t.Errorf("key %q must be accepted: %v", key, err)
		}
	}
}

func TestIngestOutboxFailureSurfaces(t *testing.T) {
	t.Parallel()
	f := newIngestFixture(t)
	f.outbox.failOn = errors.New("disk full")
	if _, err := f.svc.Ingest(context.Background(), f.input(`{}`)); err == nil {
		t.Fatal("an outbox write failure must fail the ingest, never accept the event silently")
	}
	if f.count("accepted") != 0 {
		t.Error("a failed ingest must not count as accepted")
	}
}
