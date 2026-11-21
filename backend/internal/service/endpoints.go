package service

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform"
	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/platform/secretbox"
)

const (
	maxURLLength        = 2048
	maxEndpointSubs     = 100
	maxEndpointDescribe = 500
)

// EndpointService manages the URLs that receive webhooks (FR-E2–E4).
type EndpointService struct {
	eps       domain.EndpointRepository
	consumers domain.ConsumerRepository
	types     domain.EventTypeRepository
	box       *secretbox.Box
	clock     platform.Clock
}

// NewEndpointService returns an EndpointService. box encrypts signing secrets at rest.
func NewEndpointService(eps domain.EndpointRepository, consumers domain.ConsumerRepository, types domain.EventTypeRepository, box *secretbox.Box, clock platform.Clock) *EndpointService {
	return &EndpointService{eps: eps, consumers: consumers, types: types, box: box, clock: clock}
}

// EndpointInput creates an endpoint. Enabled defaults to true; no EventTypes means "all".
type EndpointInput struct {
	URL         string
	Description string
	EventTypes  []string
	Enabled     *bool
}

// EndpointPatch changes an endpoint. A nil field keeps the stored value; a non-nil empty
// EventTypes means "all event types".
type EndpointPatch struct {
	URL         *string
	Description *string
	EventTypes  *[]string
	Enabled     *bool
}

// Create adds an endpoint under one of the org's consumers and returns its signing secret,
// which is shown only now and on rotation.
func (s *EndpointService) Create(ctx context.Context, orgID, consumerID uuid.UUID, in EndpointInput) (domain.Endpoint, string, error) {
	if _, err := s.consumers.Get(ctx, orgID, consumerID); err != nil {
		return domain.Endpoint{}, "", fmt.Errorf("create endpoint: %w", err)
	}
	ve := &domain.ValidationError{}
	ve.Fields = append(ve.Fields, validateURL(in.URL)...)
	if len(in.Description) > maxEndpointDescribe {
		ve.Fields = append(ve.Fields, domain.FieldError{Field: fieldDescription, Issue: issueTooLong500})
	}
	subs, subFields := normalizeSubscriptions(in.EventTypes)
	ve.Fields = append(ve.Fields, subFields...)
	if len(ve.Fields) > 0 {
		return domain.Endpoint{}, "", ve
	}
	if err := s.checkSubscriptionsExist(ctx, orgID, subs); err != nil {
		return domain.Endpoint{}, "", err
	}

	secret, err := secretbox.NewSigningSecret()
	if err != nil {
		return domain.Endpoint{}, "", err
	}
	sealed, err := s.box.Seal([]byte(secret))
	if err != nil {
		return domain.Endpoint{}, "", err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	now := s.clock.Now()
	ep := domain.Endpoint{
		ID: platform.NewID(), OrgID: orgID, ConsumerID: consumerID, URL: in.URL, Description: in.Description,
		EventTypes: subs, Enabled: enabled, SecretCiphertext: sealed, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.eps.Create(ctx, ep); err != nil {
		return domain.Endpoint{}, "", fmt.Errorf("create endpoint: %w", err)
	}
	return ep, secret, nil
}

// Get returns one endpoint of the org.
func (s *EndpointService) Get(ctx context.Context, orgID, id uuid.UUID) (domain.Endpoint, error) {
	ep, err := s.eps.Get(ctx, orgID, id)
	if err != nil {
		return domain.Endpoint{}, fmt.Errorf("get endpoint: %w", err)
	}
	return ep, nil
}

// ListByConsumer returns one page of a consumer's endpoints and the next cursor.
func (s *EndpointService) ListByConsumer(ctx context.Context, orgID, consumerID uuid.UUID, p domain.Page) ([]domain.Endpoint, *domain.Cursor, error) {
	if _, err := s.consumers.Get(ctx, orgID, consumerID); err != nil {
		return nil, nil, fmt.Errorf("list endpoints: %w", err)
	}
	rows, err := s.eps.ListByConsumer(ctx, orgID, consumerID, domain.Page{Limit: p.Limit + 1, After: p.After})
	if err != nil {
		return nil, nil, fmt.Errorf("list endpoints: %w", err)
	}
	rows, next := trimPage(rows, p.Limit, func(e domain.Endpoint) domain.Cursor {
		return domain.Cursor{CreatedAt: e.CreatedAt, ID: e.ID}
	})
	return rows, next, nil
}

// Update applies a patch (FR-E2, FR-E4).
func (s *EndpointService) Update(ctx context.Context, orgID, id uuid.UUID, patch EndpointPatch) (domain.Endpoint, error) {
	ep, err := s.eps.Get(ctx, orgID, id)
	if err != nil {
		return domain.Endpoint{}, fmt.Errorf("update endpoint: %w", err)
	}
	ve := &domain.ValidationError{}
	if patch.URL != nil {
		ve.Fields = append(ve.Fields, validateURL(*patch.URL)...)
		ep.URL = *patch.URL
	}
	if patch.Description != nil {
		if len(*patch.Description) > maxEndpointDescribe {
			ve.Fields = append(ve.Fields, domain.FieldError{Field: fieldDescription, Issue: issueTooLong500})
		}
		ep.Description = *patch.Description
	}
	if patch.EventTypes != nil {
		subs, subFields := normalizeSubscriptions(*patch.EventTypes)
		ve.Fields = append(ve.Fields, subFields...)
		ep.EventTypes = subs
	}
	if patch.Enabled != nil {
		ep.Enabled = *patch.Enabled
	}
	if len(ve.Fields) > 0 {
		return domain.Endpoint{}, ve
	}
	if patch.EventTypes != nil {
		if err := s.checkSubscriptionsExist(ctx, orgID, ep.EventTypes); err != nil {
			return domain.Endpoint{}, err
		}
	}
	if err := s.eps.Update(ctx, ep); err != nil {
		return domain.Endpoint{}, fmt.Errorf("update endpoint: %w", err)
	}
	ep.UpdatedAt = s.clock.Now()
	return ep, nil
}

// RotateSecret issues a new signing secret and returns it once (FR-E3).
func (s *EndpointService) RotateSecret(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	secret, err := secretbox.NewSigningSecret()
	if err != nil {
		return "", err
	}
	sealed, err := s.box.Seal([]byte(secret))
	if err != nil {
		return "", err
	}
	if err := s.eps.UpdateSecret(ctx, orgID, id, sealed); err != nil {
		return "", fmt.Errorf("rotate endpoint secret: %w", err)
	}
	return secret, nil
}

// Delete removes an endpoint.
func (s *EndpointService) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	if err := s.eps.Delete(ctx, orgID, id); err != nil {
		return fmt.Errorf("delete endpoint: %w", err)
	}
	return nil
}

// checkSubscriptionsExist is a 422 when a subscribed event type does not exist in the org.
func (s *EndpointService) checkSubscriptionsExist(ctx context.Context, orgID uuid.UUID, names []string) error {
	if len(names) == 0 {
		return nil
	}
	found, err := s.types.GetByNames(ctx, orgID, names)
	if err != nil {
		return fmt.Errorf("check event types: %w", err)
	}
	have := make(map[string]bool, len(found))
	for _, et := range found {
		have[et.Name] = true
	}
	var unknown []string
	for _, n := range names {
		if !have[n] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return &domain.ValidationError{
		Cause:  domain.ErrUnprocessable,
		Fields: []domain.FieldError{{Field: fieldEventTypes, Issue: "unknown event types: " + strings.Join(unknown, ", ")}},
	}
}

// normalizeSubscriptions deduplicates (keeping first-seen order) and rejects blank names.
func normalizeSubscriptions(names []string) ([]string, []domain.FieldError) {
	if len(names) > maxEndpointSubs {
		return nil, []domain.FieldError{{Field: fieldEventTypes, Issue: "must list at most 100 event types"}}
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			return nil, []domain.FieldError{{Field: fieldEventTypes, Issue: "must not contain blank names"}}
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// validateURL accepts absolute http(s) URLs with a host and no embedded credentials.
// https-only for non-local hosts arrives with FR-E5 in M2.
func validateURL(raw string) []domain.FieldError {
	bad := func(issue string) []domain.FieldError { return []domain.FieldError{{Field: "url", Issue: issue}} }
	if raw == "" || len(raw) > maxURLLength {
		return bad("must be 1 to 2048 characters")
	}
	if strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f || r == ' ' }) {
		return bad("must not contain spaces or control characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return bad("must be a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return bad("must start with http:// or https://")
	}
	if u.Hostname() == "" {
		return bad("must include a host")
	}
	if u.User != nil {
		return bad("must not contain credentials")
	}
	return nil
}
