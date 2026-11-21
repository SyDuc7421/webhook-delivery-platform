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

type fakeAPIKeys struct {
	mu     sync.Mutex
	rows   []domain.APIKey
	hashes map[string]uuid.UUID // hex of hash → key id
	calls  int
}

func newFakeAPIKeys() *fakeAPIKeys { return &fakeAPIKeys{hashes: map[string]uuid.UUID{}} }

func (f *fakeAPIKeys) Create(_ context.Context, k domain.APIKey, hash []byte, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, k)
	f.hashes[string(hash)] = k.ID
	return nil
}

func (f *fakeAPIKeys) List(_ context.Context, orgID uuid.UUID, p domain.Page) ([]domain.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.APIKey
	// newest first
	for i := len(f.rows) - 1; i >= 0; i-- {
		k := f.rows[i]
		if k.OrgID != orgID {
			continue
		}
		if p.After != nil && !sortsAfter(k.CreatedAt, k.ID, *p.After) {
			continue
		}
		out = append(out, k)
		if len(out) == p.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeAPIKeys) Revoke(_ context.Context, orgID, id uuid.UUID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id && f.rows[i].OrgID == orgID && f.rows[i].RevokedAt == nil {
			f.rows[i].RevokedAt = &at
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeAPIKeys) GetActiveByHash(_ context.Context, hash []byte) (domain.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	id, ok := f.hashes[string(hash)]
	if !ok {
		return domain.APIKey{}, domain.ErrNotFound
	}
	for _, k := range f.rows {
		if k.ID == id && k.RevokedAt == nil {
			return k, nil
		}
	}
	return domain.APIKey{}, domain.ErrNotFound
}

// sortsAfter reports whether a row sorts strictly after the cursor in (created_at, id) DESC order.
func sortsAfter(createdAt time.Time, id uuid.UUID, c domain.Cursor) bool {
	return createdAt.Before(c.CreatedAt) || (createdAt.Equal(c.CreatedAt) && id.String() < c.ID.String())
}

type fakeEventTypes struct {
	mu   sync.Mutex
	rows []domain.EventType
}

func newFakeEventTypes() *fakeEventTypes { return &fakeEventTypes{} }

func (f *fakeEventTypes) Create(_ context.Context, e domain.EventType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == e.OrgID && r.Name == e.Name {
			return domain.ErrAlreadyExists
		}
	}
	f.rows = append(f.rows, e)
	return nil
}

func (f *fakeEventTypes) Get(_ context.Context, orgID, id uuid.UUID) (domain.EventType, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			return r, nil
		}
	}
	return domain.EventType{}, domain.ErrNotFound
}

func (f *fakeEventTypes) GetByNames(_ context.Context, orgID uuid.UUID, names []string) ([]domain.EventType, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.EventType
	for _, r := range f.rows {
		for _, n := range names {
			if r.OrgID == orgID && r.Name == n {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (f *fakeEventTypes) List(_ context.Context, orgID uuid.UUID, p domain.Page) ([]domain.EventType, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.EventType
	for i := len(f.rows) - 1; i >= 0; i-- {
		r := f.rows[i]
		if r.OrgID != orgID {
			continue
		}
		if p.After != nil && !sortsAfter(r.CreatedAt, r.ID, *p.After) {
			continue
		}
		out = append(out, r)
		if len(out) == p.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeEventTypes) Update(_ context.Context, e domain.EventType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == e.OrgID && r.ID == e.ID {
			f.rows[i].Description, f.rows[i].ExamplePayload = e.Description, e.ExamplePayload
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeEventTypes) Delete(_ context.Context, orgID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type fakeConsumers struct {
	mu   sync.Mutex
	rows []domain.Consumer
}

func newFakeConsumers() *fakeConsumers { return &fakeConsumers{} }

func (f *fakeConsumers) Create(_ context.Context, c domain.Consumer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == c.OrgID && r.UID == c.UID {
			return domain.ErrAlreadyExists
		}
	}
	f.rows = append(f.rows, c)
	return nil
}

func (f *fakeConsumers) Get(_ context.Context, orgID, id uuid.UUID) (domain.Consumer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			return r, nil
		}
	}
	return domain.Consumer{}, domain.ErrNotFound
}

func (f *fakeConsumers) GetByUID(_ context.Context, orgID uuid.UUID, uid string) (domain.Consumer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.UID == uid {
			return r, nil
		}
	}
	return domain.Consumer{}, domain.ErrNotFound
}

func (f *fakeConsumers) List(_ context.Context, orgID uuid.UUID, p domain.Page) ([]domain.Consumer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Consumer
	for i := len(f.rows) - 1; i >= 0; i-- {
		r := f.rows[i]
		if r.OrgID != orgID {
			continue
		}
		if p.After != nil && !sortsAfter(r.CreatedAt, r.ID, *p.After) {
			continue
		}
		out = append(out, r)
		if len(out) == p.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeConsumers) Update(_ context.Context, c domain.Consumer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == c.OrgID && r.ID == c.ID {
			f.rows[i].Name = c.Name
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeConsumers) Delete(_ context.Context, orgID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type fakeEndpoints struct {
	mu   sync.Mutex
	rows []domain.Endpoint
}

func newFakeEndpoints() *fakeEndpoints { return &fakeEndpoints{} }

func (f *fakeEndpoints) Create(_ context.Context, e domain.Endpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, e)
	return nil
}

func (f *fakeEndpoints) Get(_ context.Context, orgID, id uuid.UUID) (domain.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			return r, nil
		}
	}
	return domain.Endpoint{}, domain.ErrNotFound
}

func (f *fakeEndpoints) ListByConsumer(_ context.Context, orgID, consumerID uuid.UUID, p domain.Page) ([]domain.Endpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Endpoint
	for i := len(f.rows) - 1; i >= 0; i-- {
		r := f.rows[i]
		if r.OrgID != orgID || r.ConsumerID != consumerID {
			continue
		}
		if p.After != nil && !sortsAfter(r.CreatedAt, r.ID, *p.After) {
			continue
		}
		out = append(out, r)
		if len(out) == p.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeEndpoints) Update(_ context.Context, e domain.Endpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == e.OrgID && r.ID == e.ID {
			f.rows[i].URL, f.rows[i].Description, f.rows[i].EventTypes, f.rows[i].Enabled = e.URL, e.Description, e.EventTypes, e.Enabled
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeEndpoints) UpdateSecret(_ context.Context, orgID, id uuid.UUID, ct []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			f.rows[i].SecretCiphertext = ct
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeEndpoints) Delete(_ context.Context, orgID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.OrgID == orgID && r.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type fakeEvents struct {
	mu   sync.Mutex
	rows []domain.Event
}

func newFakeEvents() *fakeEvents { return &fakeEvents{} }

func (f *fakeEvents) Insert(_ context.Context, e domain.Event) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.IdempotencyKey != nil {
		for _, r := range f.rows {
			if r.OrgID == e.OrgID && r.IdempotencyKey != nil && *r.IdempotencyKey == *e.IdempotencyKey {
				return false, nil
			}
		}
	}
	f.rows = append(f.rows, e)
	return true, nil
}

func (f *fakeEvents) GetByIdempotencyKey(_ context.Context, orgID uuid.UUID, key string) (domain.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.IdempotencyKey != nil && *r.IdempotencyKey == key {
			return r, nil
		}
	}
	return domain.Event{}, domain.ErrNotFound
}

type fakeOutbox struct {
	mu      sync.Mutex
	queued  []uuid.UUID
	failOn  error
	deleted []int64
	batch   []domain.OutboxRecord
}

func (f *fakeOutbox) Enqueue(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn != nil {
		return f.failOn
	}
	f.queued = append(f.queued, id)
	return nil
}

func (f *fakeOutbox) LockBatch(_ context.Context, limit int) ([]domain.OutboxRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.batch) > limit {
		return f.batch[:limit], nil
	}
	return f.batch, nil
}

func (f *fakeOutbox) Delete(_ context.Context, ids []int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, ids...)
	return nil
}

func (f *fakeOutbox) Backlog(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.queued)), nil
}
