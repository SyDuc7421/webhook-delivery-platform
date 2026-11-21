package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

func newAPIKeySvc() (*APIKeyService, *fakeAPIKeys, *fakeClock) {
	repo := newFakeAPIKeys()
	clock := &fakeClock{now: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)}
	return NewAPIKeyService(repo, clock), repo, clock
}

func TestAPIKeyCreateStoresOnlyTheHash(t *testing.T) {
	t.Parallel()
	svc, repo, _ := newAPIKeySvc()
	org, user := uuid.New(), uuid.New()
	k, full, err := svc.Create(context.Background(), org, user, " ci pipeline ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(full, "whk_") || k.Name != "ci pipeline" || k.OrgID != org || !strings.HasPrefix(full, k.Prefix) {
		t.Errorf("key = %+v full = %q", k, full)
	}
	want := sha256.Sum256([]byte(full))
	if _, ok := repo.hashes[string(want[:])]; !ok {
		t.Error("repository must hold sha256(full key)")
	}
	for _, row := range repo.rows {
		if strings.Contains(row.Prefix+row.Name, full) {
			t.Error("the full key must not be stored anywhere")
		}
	}
}

func TestAPIKeyCreateValidatesName(t *testing.T) {
	t.Parallel()
	svc, _, _ := newAPIKeySvc()
	for _, name := range []string{"", "   ", strings.Repeat("a", 101)} {
		_, _, err := svc.Create(context.Background(), uuid.New(), uuid.New(), name)
		if _, ok := fieldsOf(t, err)["name"]; !ok {
			t.Errorf("name %q: err = %v, want field name", name, err)
		}
	}
}

func TestAPIKeyAuthenticate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newAPIKeySvc()
	org := uuid.New()
	k, full, err := svc.Create(ctx, org, uuid.New(), "k")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.Authenticate(ctx, full)
	if err != nil || got != org {
		t.Fatalf("authenticate = %v, %v, want %v", got, err, org)
	}

	calls := repo.calls
	for _, bad := range []string{"", "garbage", "Bearer " + full, "whk_" + strings.Repeat("A", 43)} {
		if _, err := svc.Authenticate(ctx, bad); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("Authenticate(%q) err = %v, want ErrUnauthorized", bad, err)
		}
	}
	if repo.calls-calls != 1 {
		t.Errorf("only the well-formed unknown key may reach the repository, got %d lookups", repo.calls-calls)
	}

	if err := svc.Revoke(ctx, org, k.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Authenticate(ctx, full); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("revoked key err = %v, want ErrUnauthorized (FR-K3)", err)
	}
	if err := svc.Revoke(ctx, uuid.New(), k.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other org revoke err = %v, want ErrNotFound", err)
	}
}

func TestAPIKeyListPaginates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, clock := newAPIKeySvc()
	org := uuid.New()
	for range 5 {
		clock.now = clock.now.Add(time.Second)
		if _, _, err := svc.Create(ctx, org, uuid.New(), "k"); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	page1, next, err := svc.List(ctx, org, domain.Page{Limit: 2})
	if err != nil || len(page1) != 2 || next == nil {
		t.Fatalf("page1 = %d rows, next = %v, err = %v", len(page1), next, err)
	}
	page2, next2, err := svc.List(ctx, org, domain.Page{Limit: 2, After: next})
	if err != nil || len(page2) != 2 || next2 == nil {
		t.Fatalf("page2 = %d rows, next = %v, err = %v", len(page2), next2, err)
	}
	page3, next3, err := svc.List(ctx, org, domain.Page{Limit: 2, After: next2})
	if err != nil || len(page3) != 1 || next3 != nil {
		t.Fatalf("page3 = %d rows, next = %v, err = %v; the last page has no cursor", len(page3), next3, err)
	}
}
