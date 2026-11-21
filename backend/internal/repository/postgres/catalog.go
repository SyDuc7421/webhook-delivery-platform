package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/repository/postgres/db"
)

// limit32 converts a page limit (capped by the service at 101) for sqlc.
func limit32(p domain.Page) int32 { return int32(p.Limit) } //nolint:gosec // capped at 101 by the service

// ---- event types ----

// EventTypeRepo implements domain.EventTypeRepository.
type EventTypeRepo struct{ pool *pgxpool.Pool }

// NewEventTypeRepo returns an EventTypeRepo.
func NewEventTypeRepo(pool *pgxpool.Pool) *EventTypeRepo { return &EventTypeRepo{pool: pool} }

// Create inserts an event type.
func (r *EventTypeRepo) Create(ctx context.Context, e domain.EventType) error {
	err := queries(ctx, r.pool).CreateEventType(ctx, db.CreateEventTypeParams{
		ID: e.ID, OrgID: e.OrgID, Name: e.Name, Description: e.Description, ExamplePayload: e.ExamplePayload,
	})
	if err != nil {
		return fmt.Errorf("create event type: %w", mapErr(err))
	}
	return nil
}

// Get returns one event type of the org.
func (r *EventTypeRepo) Get(ctx context.Context, orgID, id uuid.UUID) (domain.EventType, error) {
	row, err := queries(ctx, r.pool).GetEventType(ctx, db.GetEventTypeParams{OrgID: orgID, ID: id})
	if err != nil {
		return domain.EventType{}, fmt.Errorf("get event type: %w", mapErr(err))
	}
	return eventTypeFromModel(row), nil
}

// GetByNames returns the org's event types with these names; unknown names are simply absent.
func (r *EventTypeRepo) GetByNames(ctx context.Context, orgID uuid.UUID, names []string) ([]domain.EventType, error) {
	rows, err := queries(ctx, r.pool).GetEventTypesByNames(ctx, db.GetEventTypesByNamesParams{OrgID: orgID, Column2: names})
	if err != nil {
		return nil, fmt.Errorf("get event types by names: %w", mapErr(err))
	}
	return mapSlice(rows, eventTypeFromModel), nil
}

// List returns a keyset page, newest first.
func (r *EventTypeRepo) List(ctx context.Context, orgID uuid.UUID, p domain.Page) ([]domain.EventType, error) {
	q := queries(ctx, r.pool)
	var rows []db.EventType
	var err error
	if p.After == nil {
		rows, err = q.ListEventTypes(ctx, db.ListEventTypesParams{OrgID: orgID, Limit: limit32(p)})
	} else {
		rows, err = q.ListEventTypesAfter(ctx, db.ListEventTypesAfterParams{
			OrgID: orgID, AfterCreatedAt: p.After.CreatedAt, AfterID: p.After.ID, PageLimit: limit32(p),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("list event types: %w", mapErr(err))
	}
	return mapSlice(rows, eventTypeFromModel), nil
}

// Update changes description and example payload.
func (r *EventTypeRepo) Update(ctx context.Context, e domain.EventType) error {
	n, err := queries(ctx, r.pool).UpdateEventType(ctx, db.UpdateEventTypeParams{
		OrgID: e.OrgID, ID: e.ID, Description: e.Description, ExamplePayload: e.ExamplePayload,
	})
	if err != nil {
		return fmt.Errorf("update event type: %w", mapErr(err))
	}
	return notFoundIfNone(n, "event type")
}

// Delete removes an event type of the org.
func (r *EventTypeRepo) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	n, err := queries(ctx, r.pool).DeleteEventType(ctx, db.DeleteEventTypeParams{OrgID: orgID, ID: id})
	if err != nil {
		return fmt.Errorf("delete event type: %w", mapErr(err))
	}
	return notFoundIfNone(n, "event type")
}

func eventTypeFromModel(m db.EventType) domain.EventType {
	return domain.EventType{
		ID: m.ID, OrgID: m.OrgID, Name: m.Name, Description: m.Description,
		ExamplePayload: m.ExamplePayload, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// ---- consumers ----

// ConsumerRepo implements domain.ConsumerRepository.
type ConsumerRepo struct{ pool *pgxpool.Pool }

// NewConsumerRepo returns a ConsumerRepo.
func NewConsumerRepo(pool *pgxpool.Pool) *ConsumerRepo { return &ConsumerRepo{pool: pool} }

// Create inserts a consumer.
func (r *ConsumerRepo) Create(ctx context.Context, c domain.Consumer) error {
	err := queries(ctx, r.pool).CreateConsumer(ctx, db.CreateConsumerParams{ID: c.ID, OrgID: c.OrgID, Uid: c.UID, Name: c.Name})
	if err != nil {
		return fmt.Errorf("create consumer: %w", mapErr(err))
	}
	return nil
}

// Get returns one consumer of the org.
func (r *ConsumerRepo) Get(ctx context.Context, orgID, id uuid.UUID) (domain.Consumer, error) {
	row, err := queries(ctx, r.pool).GetConsumer(ctx, db.GetConsumerParams{OrgID: orgID, ID: id})
	if err != nil {
		return domain.Consumer{}, fmt.Errorf("get consumer: %w", mapErr(err))
	}
	return consumerFromModel(row), nil
}

// GetByUID returns the org's consumer with this uid.
func (r *ConsumerRepo) GetByUID(ctx context.Context, orgID uuid.UUID, uid string) (domain.Consumer, error) {
	row, err := queries(ctx, r.pool).GetConsumerByUID(ctx, db.GetConsumerByUIDParams{OrgID: orgID, Uid: uid})
	if err != nil {
		return domain.Consumer{}, fmt.Errorf("get consumer by uid: %w", mapErr(err))
	}
	return consumerFromModel(row), nil
}

// List returns a keyset page, newest first.
func (r *ConsumerRepo) List(ctx context.Context, orgID uuid.UUID, p domain.Page) ([]domain.Consumer, error) {
	q := queries(ctx, r.pool)
	var rows []db.Consumer
	var err error
	if p.After == nil {
		rows, err = q.ListConsumers(ctx, db.ListConsumersParams{OrgID: orgID, Limit: limit32(p)})
	} else {
		rows, err = q.ListConsumersAfter(ctx, db.ListConsumersAfterParams{
			OrgID: orgID, AfterCreatedAt: p.After.CreatedAt, AfterID: p.After.ID, PageLimit: limit32(p),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("list consumers: %w", mapErr(err))
	}
	return mapSlice(rows, consumerFromModel), nil
}

// Update changes the display name (the uid is immutable).
func (r *ConsumerRepo) Update(ctx context.Context, c domain.Consumer) error {
	n, err := queries(ctx, r.pool).UpdateConsumer(ctx, db.UpdateConsumerParams{OrgID: c.OrgID, ID: c.ID, Name: c.Name})
	if err != nil {
		return fmt.Errorf("update consumer: %w", mapErr(err))
	}
	return notFoundIfNone(n, "consumer")
}

// Delete removes a consumer and, by cascade, its endpoints.
func (r *ConsumerRepo) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	n, err := queries(ctx, r.pool).DeleteConsumer(ctx, db.DeleteConsumerParams{OrgID: orgID, ID: id})
	if err != nil {
		return fmt.Errorf("delete consumer: %w", mapErr(err))
	}
	return notFoundIfNone(n, "consumer")
}

func consumerFromModel(m db.Consumer) domain.Consumer {
	return domain.Consumer{ID: m.ID, OrgID: m.OrgID, UID: m.Uid, Name: m.Name, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

// ---- endpoints ----

// EndpointRepo implements domain.EndpointRepository.
type EndpointRepo struct{ pool *pgxpool.Pool }

// NewEndpointRepo returns an EndpointRepo.
func NewEndpointRepo(pool *pgxpool.Pool) *EndpointRepo { return &EndpointRepo{pool: pool} }

// Create inserts an endpoint.
func (r *EndpointRepo) Create(ctx context.Context, e domain.Endpoint) error {
	err := queries(ctx, r.pool).CreateEndpoint(ctx, db.CreateEndpointParams{
		ID: e.ID, OrgID: e.OrgID, ConsumerID: e.ConsumerID, Url: e.URL, Description: e.Description,
		EventTypes: nonNil(e.EventTypes), SecretCiphertext: e.SecretCiphertext, Enabled: e.Enabled,
	})
	if err != nil {
		return fmt.Errorf("create endpoint: %w", mapErr(err))
	}
	return nil
}

// Get returns one endpoint of the org.
func (r *EndpointRepo) Get(ctx context.Context, orgID, id uuid.UUID) (domain.Endpoint, error) {
	row, err := queries(ctx, r.pool).GetEndpoint(ctx, db.GetEndpointParams{OrgID: orgID, ID: id})
	if err != nil {
		return domain.Endpoint{}, fmt.Errorf("get endpoint: %w", mapErr(err))
	}
	return endpointFromModel(row), nil
}

// ListByConsumer returns a keyset page of one consumer's endpoints, newest first.
func (r *EndpointRepo) ListByConsumer(ctx context.Context, orgID, consumerID uuid.UUID, p domain.Page) ([]domain.Endpoint, error) {
	q := queries(ctx, r.pool)
	var rows []db.Endpoint
	var err error
	if p.After == nil {
		rows, err = q.ListEndpointsByConsumer(ctx, db.ListEndpointsByConsumerParams{OrgID: orgID, ConsumerID: consumerID, Limit: limit32(p)})
	} else {
		rows, err = q.ListEndpointsByConsumerAfter(ctx, db.ListEndpointsByConsumerAfterParams{
			OrgID: orgID, ConsumerID: consumerID, AfterCreatedAt: p.After.CreatedAt, AfterID: p.After.ID, PageLimit: limit32(p),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", mapErr(err))
	}
	return mapSlice(rows, endpointFromModel), nil
}

// Update changes url, description, subscriptions and enabled flag (not the secret).
func (r *EndpointRepo) Update(ctx context.Context, e domain.Endpoint) error {
	n, err := queries(ctx, r.pool).UpdateEndpoint(ctx, db.UpdateEndpointParams{
		OrgID: e.OrgID, ID: e.ID, Url: e.URL, Description: e.Description, EventTypes: nonNil(e.EventTypes), Enabled: e.Enabled,
	})
	if err != nil {
		return fmt.Errorf("update endpoint: %w", mapErr(err))
	}
	return notFoundIfNone(n, "endpoint")
}

// UpdateSecret replaces the sealed signing secret (FR-E3).
func (r *EndpointRepo) UpdateSecret(ctx context.Context, orgID, id uuid.UUID, ciphertext []byte) error {
	n, err := queries(ctx, r.pool).UpdateEndpointSecret(ctx, db.UpdateEndpointSecretParams{OrgID: orgID, ID: id, SecretCiphertext: ciphertext})
	if err != nil {
		return fmt.Errorf("update endpoint secret: %w", mapErr(err))
	}
	return notFoundIfNone(n, "endpoint")
}

// Delete removes an endpoint of the org.
func (r *EndpointRepo) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	n, err := queries(ctx, r.pool).DeleteEndpoint(ctx, db.DeleteEndpointParams{OrgID: orgID, ID: id})
	if err != nil {
		return fmt.Errorf("delete endpoint: %w", mapErr(err))
	}
	return notFoundIfNone(n, "endpoint")
}

func endpointFromModel(m db.Endpoint) domain.Endpoint {
	return domain.Endpoint{
		ID: m.ID, OrgID: m.OrgID, ConsumerID: m.ConsumerID, URL: m.Url, Description: m.Description,
		EventTypes: m.EventTypes, Enabled: m.Enabled, SecretCiphertext: m.SecretCiphertext,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// nonNil keeps a nil slice from being written as SQL NULL into a NOT NULL array column.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func mapSlice[In, Out any](in []In, f func(In) Out) []Out {
	out := make([]Out, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}
