CREATE TABLE api_keys (
    id uuid PRIMARY KEY,
    org_id uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    -- visible, non-secret start of the key, shown in the dashboard to tell keys apart
    prefix text NOT NULL,
    -- sha256 of the full key; the full key is never stored (FR-K2)
    key_hash bytea NOT NULL,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

CREATE UNIQUE INDEX api_keys_key_hash_key ON api_keys (key_hash);
CREATE INDEX api_keys_org_created_idx ON api_keys (org_id, created_at DESC, id DESC);
