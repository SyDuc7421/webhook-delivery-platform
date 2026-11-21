-- name: CreateAPIKey :exec
INSERT INTO api_keys (id, org_id, name, prefix, key_hash, created_by) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAPIKeys :many
SELECT id, org_id, name, prefix, created_at, revoked_at FROM api_keys
WHERE org_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: ListAPIKeysAfter :many
SELECT id, org_id, name, prefix, created_at, revoked_at FROM api_keys
WHERE org_id = $1 AND (created_at, id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = $3 WHERE org_id = $1 AND id = $2 AND revoked_at IS NULL;

-- name: GetActiveAPIKeyByHash :one
SELECT id, org_id, name, prefix, created_at, revoked_at FROM api_keys
WHERE key_hash = $1 AND revoked_at IS NULL;
