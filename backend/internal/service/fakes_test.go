package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

// fakeTx runs fn directly; fakes below are not transactional, which is enough for unit tests.
type fakeTx struct{}

func (fakeTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

type fakeOrgs struct {
	mu   sync.Mutex
	byID map[uuid.UUID]domain.Org
}

func newFakeOrgs() *fakeOrgs { return &fakeOrgs{byID: map[uuid.UUID]domain.Org{}} }

func (f *fakeOrgs) Create(_ context.Context, o domain.Org) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[o.ID] = o
	return nil
}

func (f *fakeOrgs) Get(_ context.Context, id uuid.UUID) (domain.Org, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.byID[id]
	if !ok {
		return domain.Org{}, domain.ErrNotFound
	}
	return o, nil
}

type fakeUsers struct {
	mu      sync.Mutex
	byEmail map[string]domain.User
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byEmail: map[string]domain.User{}} }

func (f *fakeUsers) Create(_ context.Context, u domain.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byEmail[u.Email]; ok {
		return domain.ErrAlreadyExists
	}
	f.byEmail[u.Email] = u
	return nil
}

func (f *fakeUsers) GetByEmail(_ context.Context, email string) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byEmail[email]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) Get(_ context.Context, id uuid.UUID) (domain.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.byEmail {
		if u.ID == id {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

type fakeRefresh struct {
	mu   sync.Mutex
	m    map[string]uuid.UUID
	down bool
}

func newFakeRefresh() *fakeRefresh { return &fakeRefresh{m: map[string]uuid.UUID{}} }

func (f *fakeRefresh) Save(_ context.Context, h string, id uuid.UUID, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return domain.ErrUnavailable
	}
	f.m[h] = id
	return nil
}

func (f *fakeRefresh) Consume(_ context.Context, h string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return uuid.Nil, domain.ErrUnavailable
	}
	id, ok := f.m[h]
	if !ok {
		return uuid.Nil, domain.ErrUnauthorized
	}
	delete(f.m, h)
	return id, nil
}

func (f *fakeRefresh) Delete(_ context.Context, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, h)
	return nil
}

func (f *fakeRefresh) has(h string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.m[h]
	return ok
}
