package service

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/secretbox"
)

type endpointFixture struct {
	svc       *EndpointService
	repo      *fakeEndpoints
	box       *secretbox.Box
	org       uuid.UUID
	consumer  domain.Consumer
	otherOrg  uuid.UUID
	catalog   *CatalogService
	clock     *fakeClock
	typesRepo *fakeEventTypes
}

func newEndpointFixture(t *testing.T) endpointFixture {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	types, consumers, eps := newFakeEventTypes(), newFakeConsumers(), newFakeEndpoints()
	f := endpointFixture{
		svc: NewEndpointService(eps, consumers, types, box, clock), repo: eps, box: box,
		org: uuid.New(), otherOrg: uuid.New(), catalog: NewCatalogService(types, consumers, clock), clock: clock, typesRepo: types,
	}
	c, err := f.catalog.CreateConsumer(context.Background(), f.org, "shop_42", "Shop")
	if err != nil {
		t.Fatalf("consumer: %v", err)
	}
	f.consumer = c
	for _, n := range []string{"payment.paid", "payment.failed"} {
		if _, err := f.catalog.CreateEventType(context.Background(), f.org, EventTypeInput{Name: n}); err != nil {
			t.Fatalf("event type: %v", err)
		}
	}
	return f
}

func TestCreateEndpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newEndpointFixture(t)

	ep, secret, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{
		URL: "https://example.com/hooks", EventTypes: []string{"payment.paid", "payment.paid", "payment.failed"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(secret, "whsec_") {
		t.Errorf("secret = %q", secret)
	}
	if bytes.Contains(ep.SecretCiphertext, []byte(secret)) {
		t.Error("secret must be stored encrypted")
	}
	if opened, oerr := f.box.Open(ep.SecretCiphertext); oerr != nil || string(opened) != secret {
		t.Errorf("stored ciphertext opens to %q, %v; want the returned secret", opened, oerr)
	}
	if !ep.Enabled {
		t.Error("endpoints are enabled by default (FR-E4)")
	}
	if want := []string{"payment.paid", "payment.failed"}; !slices.Equal(ep.EventTypes, want) {
		t.Errorf("event types = %v, want deduplicated %v", ep.EventTypes, want)
	}

	off := false
	disabled, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: "https://example.com/off", Enabled: &off})
	if err != nil || disabled.Enabled {
		t.Errorf("explicit enabled=false = %+v, %v", disabled, err)
	}
	if len(disabled.EventTypes) != 0 {
		t.Errorf("no subscriptions means all event types (FR-E2), got %v", disabled.EventTypes)
	}
}

func TestCreateEndpointValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newEndpointFixture(t)

	badURLs := map[string]string{
		"ftp scheme":     "ftp://example.com/x",
		"no scheme":      "example.com/x",
		"relative":       "/hook",
		"no host":        "https:///path",
		"empty":          "",
		"javascript":     "javascript:alert(1)",
		"userinfo":       "https://user:pass@example.com/x",
		"too long":       "https://example.com/" + strings.Repeat("a", 2100),
		"space in host":  "https://exa mple.com/",
		"file":           "file:///etc/passwd",
		"scheme only":    "https://",
		"control char":   "https://example.com/\x00",
		"uppercase only": "HTTPS//example.com",
	}
	for name, u := range badURLs {
		_, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: u})
		if _, ok := fieldsOf(t, err)["url"]; !ok {
			t.Errorf("%s (%q): err = %v, want field url", name, u, err)
		}
	}
	for _, u := range []string{"https://example.com", "http://localhost:3000/hook", "HTTPS://Example.com/Path?q=1"} {
		if _, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: u}); err != nil {
			t.Errorf("url %q must be accepted: %v", u, err)
		}
	}

	t.Run("unknown event types are 422 with details", func(t *testing.T) {
		_, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{
			URL: "https://example.com", EventTypes: []string{"payment.paid", "nope.one", "nope.two"},
		})
		if !errors.Is(err, domain.ErrUnprocessable) {
			t.Fatalf("err = %v, want ErrUnprocessable", err)
		}
		issue := fieldsOf(t, err)["event_types"]
		if !strings.Contains(issue, "nope.one") || !strings.Contains(issue, "nope.two") || strings.Contains(issue, "payment.paid") {
			t.Errorf("issue = %q, want it to name exactly the unknown types", issue)
		}
	})

	t.Run("event types of another org do not count", func(t *testing.T) {
		_, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: "https://example.com", EventTypes: []string{"only.in.other.org"}})
		if !errors.Is(err, domain.ErrUnprocessable) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("blank event type name", func(t *testing.T) {
		_, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: "https://example.com", EventTypes: []string{""}})
		if _, ok := fieldsOf(t, err)["event_types"]; !ok {
			t.Errorf("err = %v, want field event_types", err)
		}
	})

	t.Run("consumer must belong to the org", func(t *testing.T) {
		if _, _, err := f.svc.Create(ctx, f.otherOrg, f.consumer.ID, EndpointInput{URL: "https://example.com"}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
		if _, _, err := f.svc.Create(ctx, f.org, uuid.New(), EndpointInput{URL: "https://example.com"}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("unknown consumer err = %v, want ErrNotFound", err)
		}
	})
}

func TestUpdateRotateDeleteEndpoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newEndpointFixture(t)
	ep, secret, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: "https://example.com/a", Description: "first", EventTypes: []string{"payment.paid"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	off := false
	got, err := f.svc.Update(ctx, f.org, ep.ID, EndpointPatch{Enabled: &off})
	if err != nil || got.Enabled || got.URL != "https://example.com/a" || got.Description != "first" || len(got.EventTypes) != 1 {
		t.Errorf("disable only = %+v, %v; everything else must be kept", got, err)
	}

	newURL := "https://example.com/b"
	all := []string{}
	got, err = f.svc.Update(ctx, f.org, ep.ID, EndpointPatch{URL: &newURL, EventTypes: &all})
	if err != nil || got.URL != newURL || len(got.EventTypes) != 0 || got.Enabled {
		t.Errorf("url + clear subscriptions = %+v, %v", got, err)
	}

	bad := "ftp://x"
	if _, uerr := f.svc.Update(ctx, f.org, ep.ID, EndpointPatch{URL: &bad}); uerr == nil {
		t.Error("invalid url must be rejected on update")
	}
	unknown := []string{"nope"}
	if _, uerr := f.svc.Update(ctx, f.org, ep.ID, EndpointPatch{EventTypes: &unknown}); !errors.Is(uerr, domain.ErrUnprocessable) {
		t.Errorf("unknown event type on update err = %v, want ErrUnprocessable", uerr)
	}

	newSecret, err := f.svc.RotateSecret(ctx, f.org, ep.ID)
	if err != nil || newSecret == secret || !strings.HasPrefix(newSecret, "whsec_") {
		t.Fatalf("rotate = %q, %v", newSecret, err)
	}
	stored, _ := f.repo.Get(ctx, f.org, ep.ID)
	if opened, err := f.box.Open(stored.SecretCiphertext); err != nil || string(opened) != newSecret {
		t.Errorf("stored secret opens to %q, %v; want the rotated secret", opened, err)
	}

	t.Run("another org gets not found and changes nothing", func(t *testing.T) {
		if _, err := f.svc.Update(ctx, f.otherOrg, ep.ID, EndpointPatch{Enabled: &off}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("update err = %v", err)
		}
		if _, err := f.svc.RotateSecret(ctx, f.otherOrg, ep.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("rotate err = %v", err)
		}
		if err := f.svc.Delete(ctx, f.otherOrg, ep.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete err = %v", err)
		}
		if _, err := f.svc.Get(ctx, f.otherOrg, ep.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("get err = %v", err)
		}
		after, _ := f.repo.Get(ctx, f.org, ep.ID)
		if !bytes.Equal(after.SecretCiphertext, stored.SecretCiphertext) {
			t.Error("the other org's rotate attempt changed the secret")
		}
	})

	if err := f.svc.Delete(ctx, f.org, ep.ID); err != nil {
		t.Errorf("delete: %v", err)
	}
}

func TestListEndpointsByConsumer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newEndpointFixture(t)
	for i := range 3 {
		f.clock.now = f.clock.now.Add(time.Second)
		if _, _, err := f.svc.Create(ctx, f.org, f.consumer.ID, EndpointInput{URL: "https://example.com/" + string(rune('a'+i))}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	page1, next, err := f.svc.ListByConsumer(ctx, f.org, f.consumer.ID, domain.Page{Limit: 2})
	if err != nil || len(page1) != 2 || next == nil {
		t.Fatalf("page1 = %d next=%v err=%v", len(page1), next, err)
	}
	page2, next2, err := f.svc.ListByConsumer(ctx, f.org, f.consumer.ID, domain.Page{Limit: 2, After: next})
	if err != nil || len(page2) != 1 || next2 != nil {
		t.Fatalf("page2 = %d next=%v err=%v", len(page2), next2, err)
	}
	if _, _, err := f.svc.ListByConsumer(ctx, f.otherOrg, f.consumer.ID, domain.Page{Limit: 2}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other org list err = %v, want ErrNotFound", err)
	}
}

func TestConsumerCrud(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newEndpointFixture(t)

	for _, uid := range []string{"", "has space", "a/b", strings.Repeat("u", 129)} {
		_, err := f.catalog.CreateConsumer(ctx, f.org, uid, "n")
		if _, ok := fieldsOf(t, err)["uid"]; !ok {
			t.Errorf("uid %q: err = %v, want field uid", uid, err)
		}
	}
	if _, err := f.catalog.CreateConsumer(ctx, f.org, "ok", strings.Repeat("n", 101)); err == nil {
		t.Error("name over 100 chars must be rejected")
	}
	if _, err := f.catalog.CreateConsumer(ctx, f.org, "shop_42", "dup"); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("duplicate uid err = %v, want ErrAlreadyExists", err)
	}
	if _, err := f.catalog.CreateConsumer(ctx, f.otherOrg, "shop_42", "same uid, other org"); err != nil {
		t.Errorf("same uid in another org: %v", err)
	}

	got, err := f.catalog.UpdateConsumer(ctx, f.org, f.consumer.ID, "Renamed")
	if err != nil || got.Name != "Renamed" || got.UID != "shop_42" {
		t.Errorf("update = %+v, %v; uid is immutable", got, err)
	}
	if _, uerr := f.catalog.UpdateConsumer(ctx, f.otherOrg, f.consumer.ID, "hijack"); !errors.Is(uerr, domain.ErrNotFound) {
		t.Errorf("cross-org update err = %v", uerr)
	}
	page, next, err := f.catalog.ListConsumers(ctx, f.org, domain.Page{Limit: 10})
	if err != nil || len(page) != 1 || next != nil {
		t.Errorf("list = %d next=%v err=%v", len(page), next, err)
	}
	if err := f.catalog.DeleteConsumer(ctx, f.otherOrg, f.consumer.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org delete err = %v", err)
	}
	if err := f.catalog.DeleteConsumer(ctx, f.org, f.consumer.ID); err != nil {
		t.Errorf("delete: %v", err)
	}
	if _, err := f.catalog.GetConsumer(ctx, f.org, f.consumer.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete err = %v", err)
	}
}
