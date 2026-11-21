package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
)

// maxExamplePayloadBytes matches the ingest payload cap (FR-I2): an example must be sendable.
const maxExamplePayloadBytes = 262144

// Field names used in validation details (shared by the catalog and endpoint services).
const (
	fieldDescription = "description"
	fieldEventTypes  = "event_types"

	issueTooLong500 = "must be at most 500 characters"
)

var eventTypeNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,99}$`)

// CatalogService manages event types and consumers (FR-T1–T3, FR-E1).
type CatalogService struct {
	types     domain.EventTypeRepository
	consumers domain.ConsumerRepository
	clock     platform.Clock
}

// NewCatalogService returns a CatalogService.
func NewCatalogService(types domain.EventTypeRepository, consumers domain.ConsumerRepository, clock platform.Clock) *CatalogService {
	return &CatalogService{types: types, consumers: consumers, clock: clock}
}

// EventTypeInput creates an event type. Name is immutable afterwards.
type EventTypeInput struct {
	Name           string
	Description    string
	ExamplePayload json.RawMessage
}

// CreateEventType creates an event type; the name is unique per org (FR-T1).
func (s *CatalogService) CreateEventType(ctx context.Context, orgID uuid.UUID, in EventTypeInput) (domain.EventType, error) {
	ve := &domain.ValidationError{}
	if !eventTypeNameRE.MatchString(in.Name) {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "name", Issue: "must be 1-100 chars of a-z 0-9 . _ - and start with a letter or digit"})
	}
	ve.Fields = append(ve.Fields, validateDescription(in.Description)...)
	ve.Fields = append(ve.Fields, validateExample(in.ExamplePayload)...)
	if len(ve.Fields) > 0 {
		return domain.EventType{}, ve
	}
	now := s.clock.Now()
	et := domain.EventType{
		ID: platform.NewID(), OrgID: orgID, Name: in.Name, Description: in.Description,
		ExamplePayload: in.ExamplePayload, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.types.Create(ctx, et); err != nil {
		return domain.EventType{}, fmt.Errorf("create event type: %w", err)
	}
	return et, nil
}

// GetEventType returns one event type of the org.
func (s *CatalogService) GetEventType(ctx context.Context, orgID, id uuid.UUID) (domain.EventType, error) {
	et, err := s.types.Get(ctx, orgID, id)
	if err != nil {
		return domain.EventType{}, fmt.Errorf("get event type: %w", err)
	}
	return et, nil
}

// ListEventTypes returns one page, newest first, and the next cursor (nil on the last page).
func (s *CatalogService) ListEventTypes(ctx context.Context, orgID uuid.UUID, p domain.Page) ([]domain.EventType, *domain.Cursor, error) {
	rows, err := s.types.List(ctx, orgID, domain.Page{Limit: p.Limit + 1, After: p.After})
	if err != nil {
		return nil, nil, fmt.Errorf("list event types: %w", err)
	}
	rows, next := trimPage(rows, p.Limit, func(e domain.EventType) domain.Cursor {
		return domain.Cursor{CreatedAt: e.CreatedAt, ID: e.ID}
	})
	return rows, next, nil
}

// UpdateEventType changes the description and/or the example payload. A nil argument keeps
// the stored value; a non-nil empty example clears it.
func (s *CatalogService) UpdateEventType(ctx context.Context, orgID, id uuid.UUID, description *string, example *json.RawMessage) (domain.EventType, error) {
	et, err := s.types.Get(ctx, orgID, id)
	if err != nil {
		return domain.EventType{}, fmt.Errorf("update event type: %w", err)
	}
	ve := &domain.ValidationError{}
	if description != nil {
		ve.Fields = append(ve.Fields, validateDescription(*description)...)
		et.Description = *description
	}
	if example != nil {
		ve.Fields = append(ve.Fields, validateExample(*example)...)
		et.ExamplePayload = *example
	}
	if len(ve.Fields) > 0 {
		return domain.EventType{}, ve
	}
	if err := s.types.Update(ctx, et); err != nil {
		return domain.EventType{}, fmt.Errorf("update event type: %w", err)
	}
	et.UpdatedAt = s.clock.Now()
	return et, nil
}

// DeleteEventType removes an event type.
func (s *CatalogService) DeleteEventType(ctx context.Context, orgID, id uuid.UUID) error {
	if err := s.types.Delete(ctx, orgID, id); err != nil {
		return fmt.Errorf("delete event type: %w", err)
	}
	return nil
}

func validateDescription(d string) []domain.FieldError {
	if len(d) > 500 {
		return []domain.FieldError{{Field: fieldDescription, Issue: issueTooLong500}}
	}
	return nil
}

// validateExample accepts an absent example or a JSON object up to 256 KB. It is never checked
// against a schema (FR-T3).
func validateExample(p json.RawMessage) []domain.FieldError {
	if len(p) == 0 {
		return nil
	}
	if len(p) > maxExamplePayloadBytes {
		return []domain.FieldError{{Field: "example_payload", Issue: "must be at most 256 KB"}}
	}
	if !json.Valid(p) || !strings.HasPrefix(strings.TrimSpace(string(p)), "{") {
		return []domain.FieldError{{Field: "example_payload", Issue: "must be a JSON object"}}
	}
	return nil
}

var consumerUIDRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// CreateConsumer registers one of the org's customers; the uid is the org's own id for them
// and is unique per org (FR-E1).
func (s *CatalogService) CreateConsumer(ctx context.Context, orgID uuid.UUID, uid, name string) (domain.Consumer, error) {
	ve := &domain.ValidationError{}
	if !consumerUIDRE.MatchString(uid) {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: "uid", Issue: "must be 1-128 chars of a-z A-Z 0-9 . _ : -"})
	}
	ve.Fields = append(ve.Fields, validateConsumerName(name)...)
	if len(ve.Fields) > 0 {
		return domain.Consumer{}, ve
	}
	now := s.clock.Now()
	c := domain.Consumer{ID: platform.NewID(), OrgID: orgID, UID: uid, Name: name, CreatedAt: now, UpdatedAt: now}
	if err := s.consumers.Create(ctx, c); err != nil {
		return domain.Consumer{}, fmt.Errorf("create consumer: %w", err)
	}
	return c, nil
}

// GetConsumer returns one consumer of the org.
func (s *CatalogService) GetConsumer(ctx context.Context, orgID, id uuid.UUID) (domain.Consumer, error) {
	c, err := s.consumers.Get(ctx, orgID, id)
	if err != nil {
		return domain.Consumer{}, fmt.Errorf("get consumer: %w", err)
	}
	return c, nil
}

// ListConsumers returns one page, newest first, and the next cursor (nil on the last page).
func (s *CatalogService) ListConsumers(ctx context.Context, orgID uuid.UUID, p domain.Page) ([]domain.Consumer, *domain.Cursor, error) {
	rows, err := s.consumers.List(ctx, orgID, domain.Page{Limit: p.Limit + 1, After: p.After})
	if err != nil {
		return nil, nil, fmt.Errorf("list consumers: %w", err)
	}
	rows, next := trimPage(rows, p.Limit, func(c domain.Consumer) domain.Cursor {
		return domain.Cursor{CreatedAt: c.CreatedAt, ID: c.ID}
	})
	return rows, next, nil
}

// UpdateConsumer changes the display name; the uid is immutable.
func (s *CatalogService) UpdateConsumer(ctx context.Context, orgID, id uuid.UUID, name string) (domain.Consumer, error) {
	if fields := validateConsumerName(name); len(fields) > 0 {
		return domain.Consumer{}, &domain.ValidationError{Fields: fields}
	}
	c, err := s.consumers.Get(ctx, orgID, id)
	if err != nil {
		return domain.Consumer{}, fmt.Errorf("update consumer: %w", err)
	}
	c.Name = name
	if err := s.consumers.Update(ctx, c); err != nil {
		return domain.Consumer{}, fmt.Errorf("update consumer: %w", err)
	}
	c.UpdatedAt = s.clock.Now()
	return c, nil
}

// DeleteConsumer removes a consumer and, by cascade, its endpoints.
func (s *CatalogService) DeleteConsumer(ctx context.Context, orgID, id uuid.UUID) error {
	if err := s.consumers.Delete(ctx, orgID, id); err != nil {
		return fmt.Errorf("delete consumer: %w", err)
	}
	return nil
}

func validateConsumerName(n string) []domain.FieldError {
	if len(n) > 100 {
		return []domain.FieldError{{Field: "name", Issue: "must be at most 100 characters"}}
	}
	return nil
}
