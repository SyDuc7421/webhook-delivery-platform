CREATE TABLE events (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    consumer_id uuid NOT NULL REFERENCES consumers (id) ON DELETE CASCADE,
    event_type_id uuid NOT NULL REFERENCES event_types (id) ON DELETE RESTRICT,
    -- denormalised so the event record survives renames and the relay needs no joins
    consumer_uid text NOT NULL,
    type text NOT NULL,
    payload jsonb NOT NULL,
    idempotency_key text,
    -- W3C traceparent of the ingest request, so one trace spans api -> relay -> workers (NFR-O1)
    traceparent text,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- NULL keys never conflict, so events without an Idempotency-Key are unconstrained (FR-I3).
CREATE UNIQUE INDEX events_org_idempotency_key ON events (org_id, idempotency_key);
CREATE INDEX events_org_created_idx ON events (org_id, created_at DESC, id DESC);
CREATE INDEX events_consumer_idx ON events (consumer_id);
CREATE INDEX events_event_type_idx ON events (event_type_id);

CREATE TABLE outbox (
    id bigserial PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
