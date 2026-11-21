package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/SyDuc7421/webhook-delivery-platform/backend/internal/domain"
)

// schemaVersion is the value of the schema-version record header (docs/MESSAGING.md).
const schemaVersion = "1"

// eventRecord is the value of a record on the events topic. It carries ids, never the payload:
// workers load the payload from Postgres, so secrets and disabled endpoints stay authoritative.
type eventRecord struct {
	EventID     string    `json:"event_id"`
	OrgID       string    `json:"org_id"`
	ConsumerUID string    `json:"consumer_uid"`
	Type        string    `json:"type"`
	AcceptedAt  time.Time `json:"accepted_at"`
}

// EventProducer implements domain.EventPublisher on the events topic.
type EventProducer struct {
	cl     *kgo.Client
	prefix string
}

// NewEventProducer returns a producer. cl must use acks=all (the kgo default) so that a
// returned nil means the brokers have the records.
func NewEventProducer(cl *kgo.Client, topicPrefix string) *EventProducer {
	return &EventProducer{cl: cl, prefix: topicPrefix}
}

// PublishEvents produces every event keyed by consumer_uid (so one consumer's events stay
// ordered, FR-D5) and blocks until all are acknowledged. The first failure is returned.
func (p *EventProducer) PublishEvents(ctx context.Context, events []domain.Event) error {
	topic := Name(p.prefix, TopicEvents)
	records := make([]*kgo.Record, 0, len(events))
	for _, e := range events {
		value, err := json.Marshal(eventRecord{
			EventID: e.ID.String(), OrgID: e.OrgID.String(), ConsumerUID: e.ConsumerUID, Type: e.Type, AcceptedAt: e.CreatedAt.UTC(),
		})
		if err != nil {
			return fmt.Errorf("marshal event %s: %w", e.ID, err)
		}
		headers := []kgo.RecordHeader{{Key: "schema-version", Value: []byte(schemaVersion)}}
		if e.Traceparent != "" {
			headers = append(headers, kgo.RecordHeader{Key: "traceparent", Value: []byte(e.Traceparent)})
		}
		records = append(records, &kgo.Record{Topic: topic, Key: []byte(e.ConsumerUID), Value: value, Headers: headers})
	}
	if err := p.cl.ProduceSync(ctx, records...).FirstErr(); err != nil {
		return fmt.Errorf("produce to %s: %w", topic, err)
	}
	return nil
}
