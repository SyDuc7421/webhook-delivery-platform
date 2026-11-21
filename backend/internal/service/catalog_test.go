package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

func newCatalogSvc() (*CatalogService, *fakeClock) {
	types := newFakeEventTypes()
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	return NewCatalogService(types, newFakeConsumers(), clock), clock
}

func TestCreateEventTypeValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    EventTypeInput
		field string
	}{
		{"uppercase", EventTypeInput{Name: "Payment.Paid"}, "name"},
		{"empty", EventTypeInput{Name: ""}, "name"},
		{"leading dot", EventTypeInput{Name: ".x"}, "name"},
		{"space", EventTypeInput{Name: "a b"}, "name"},
		{"too long", EventTypeInput{Name: strings.Repeat("a", 101)}, "name"},
		{"description too long", EventTypeInput{Name: "ok", Description: strings.Repeat("d", 501)}, "description"},
		{"example not json", EventTypeInput{Name: "ok", ExamplePayload: json.RawMessage(`{bad`)}, "example_payload"},
		{"example not object", EventTypeInput{Name: "ok", ExamplePayload: json.RawMessage(`"text"`)}, "example_payload"},
		{"example array", EventTypeInput{Name: "ok", ExamplePayload: json.RawMessage(`[1]`)}, "example_payload"},
		{"example nul escape", EventTypeInput{Name: "ok", ExamplePayload: json.RawMessage(`{"a":"\u0000"}`)}, "example_payload"},
		{"example lone surrogate", EventTypeInput{Name: "ok", ExamplePayload: json.RawMessage(`{"a":"\ud800"}`)}, "example_payload"},
		{"example too large", EventTypeInput{Name: "ok", ExamplePayload: json.RawMessage(`{"a":"` + strings.Repeat("x", 262144) + `"}`)}, "example_payload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newCatalogSvc()
			_, err := svc.CreateEventType(context.Background(), uuid.New(), tt.in)
			if _, ok := fieldsOf(t, err)[tt.field]; !ok {
				t.Errorf("err = %v, want field %s", err, tt.field)
			}
		})
	}
}

func TestCreateEventTypeAcceptsValidNames(t *testing.T) {
	t.Parallel()
	svc, _ := newCatalogSvc()
	for _, name := range []string{"payment.paid", "a", "0day", "user_created", "invoice-paid.v2", strings.Repeat("a", 100)} {
		if _, err := svc.CreateEventType(context.Background(), uuid.New(), EventTypeInput{Name: name}); err != nil {
			t.Errorf("name %q: %v", name, err)
		}
	}
}

func TestEventTypeDuplicateAndIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newCatalogSvc()
	orgA, orgB := uuid.New(), uuid.New()
	et, err := svc.CreateEventType(ctx, orgA, EventTypeInput{Name: "payment.paid"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreateEventType(ctx, orgA, EventTypeInput{Name: "payment.paid"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("duplicate err = %v, want ErrAlreadyExists", err)
	}
	if _, err := svc.CreateEventType(ctx, orgB, EventTypeInput{Name: "payment.paid"}); err != nil {
		t.Errorf("same name in another org: %v", err)
	}
	if _, err := svc.GetEventType(ctx, orgB, et.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org get err = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteEventType(ctx, orgB, et.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org delete err = %v, want ErrNotFound", err)
	}
}

func TestUpdateEventType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newCatalogSvc()
	org := uuid.New()
	et, err := svc.CreateEventType(ctx, org, EventTypeInput{Name: "x", Description: "first", ExamplePayload: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newDesc := "second"
	got, err := svc.UpdateEventType(ctx, org, et.ID, &newDesc, nil)
	if err != nil || got.Description != "second" || string(got.ExamplePayload) != `{"a":1}` {
		t.Errorf("description only = %+v, %v; example must be kept", got, err)
	}

	newExample := json.RawMessage(`{"b":2}`)
	got, err = svc.UpdateEventType(ctx, org, et.ID, nil, &newExample)
	if err != nil || got.Description != "second" || string(got.ExamplePayload) != `{"b":2}` {
		t.Errorf("example only = %+v, %v; description must be kept", got, err)
	}

	cleared := json.RawMessage(nil)
	got, err = svc.UpdateEventType(ctx, org, et.ID, nil, &cleared)
	if err != nil || len(got.ExamplePayload) != 0 {
		t.Errorf("clear = %+v, %v; an empty example clears it", got, err)
	}

	bad := json.RawMessage(`[1]`)
	if _, err := svc.UpdateEventType(ctx, org, et.ID, nil, &bad); err == nil {
		t.Error("invalid example must be rejected on update too")
	}
	if _, err := svc.UpdateEventType(ctx, uuid.New(), et.ID, &newDesc, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org update err = %v, want ErrNotFound", err)
	}
}

func TestListEventTypesPaginates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, clock := newCatalogSvc()
	org := uuid.New()
	for _, n := range []string{"a", "b", "c"} {
		clock.now = clock.now.Add(time.Second)
		if _, err := svc.CreateEventType(ctx, org, EventTypeInput{Name: n}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	page1, next, err := svc.ListEventTypes(ctx, org, domain.Page{Limit: 2})
	if err != nil || len(page1) != 2 || next == nil || page1[0].Name != "c" {
		t.Fatalf("page1 = %+v next=%v err=%v", page1, next, err)
	}
	page2, next2, err := svc.ListEventTypes(ctx, org, domain.Page{Limit: 2, After: next})
	if err != nil || len(page2) != 1 || next2 != nil || page2[0].Name != "a" {
		t.Fatalf("page2 = %+v next=%v err=%v", page2, next2, err)
	}
}
