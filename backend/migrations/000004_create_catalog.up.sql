CREATE TABLE event_types (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    example_payload jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);
CREATE INDEX event_types_org_created_idx ON event_types (org_id, created_at DESC, id DESC);

CREATE TABLE consumers (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    uid text NOT NULL,
    name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, uid)
);
CREATE INDEX consumers_org_created_idx ON consumers (org_id, created_at DESC, id DESC);

CREATE TABLE endpoints (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    consumer_id uuid NOT NULL REFERENCES consumers (id) ON DELETE CASCADE,
    url text NOT NULL,
    description text NOT NULL DEFAULT '',
    -- empty array = subscribed to every event type (FR-E2)
    event_types text[] NOT NULL DEFAULT '{}',
    -- AES-256-GCM sealed signing secret; the worker needs the plaintext to sign (FR-E3)
    secret_ciphertext bytea NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX endpoints_consumer_created_idx ON endpoints (consumer_id, created_at DESC, id DESC);
CREATE INDEX endpoints_consumer_enabled_idx ON endpoints (consumer_id) WHERE enabled;
