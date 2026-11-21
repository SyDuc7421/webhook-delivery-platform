-- name: CreateEventType :exec
INSERT INTO event_types (id, org_id, name, description, example_payload) VALUES ($1, $2, $3, $4, $5);

-- name: GetEventType :one
SELECT id, org_id, name, description, example_payload, created_at, updated_at FROM event_types
WHERE org_id = $1 AND id = $2;

-- name: GetEventTypesByNames :many
SELECT id, org_id, name, description, example_payload, created_at, updated_at FROM event_types
WHERE org_id = $1 AND name = ANY($2::text[]);

-- name: ListEventTypes :many
SELECT id, org_id, name, description, example_payload, created_at, updated_at FROM event_types
WHERE org_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: ListEventTypesAfter :many
SELECT id, org_id, name, description, example_payload, created_at, updated_at FROM event_types
WHERE org_id = $1 AND (created_at, id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: UpdateEventType :execrows
UPDATE event_types SET description = $3, example_payload = $4, updated_at = now() WHERE org_id = $1 AND id = $2;

-- name: DeleteEventType :execrows
DELETE FROM event_types WHERE org_id = $1 AND id = $2;

-- name: CreateConsumer :exec
INSERT INTO consumers (id, org_id, uid, name) VALUES ($1, $2, $3, $4);

-- name: GetConsumer :one
SELECT id, org_id, uid, name, created_at, updated_at FROM consumers WHERE org_id = $1 AND id = $2;

-- name: GetConsumerByUID :one
SELECT id, org_id, uid, name, created_at, updated_at FROM consumers WHERE org_id = $1 AND uid = $2;

-- name: ListConsumers :many
SELECT id, org_id, uid, name, created_at, updated_at FROM consumers
WHERE org_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2;

-- name: ListConsumersAfter :many
SELECT id, org_id, uid, name, created_at, updated_at FROM consumers
WHERE org_id = $1 AND (created_at, id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: UpdateConsumer :execrows
UPDATE consumers SET name = $3, updated_at = now() WHERE org_id = $1 AND id = $2;

-- name: DeleteConsumer :execrows
DELETE FROM consumers WHERE org_id = $1 AND id = $2;

-- name: CreateEndpoint :exec
INSERT INTO endpoints (id, org_id, consumer_id, url, description, event_types, secret_ciphertext, enabled)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetEndpoint :one
SELECT id, org_id, consumer_id, url, description, event_types, secret_ciphertext, enabled, created_at, updated_at
FROM endpoints WHERE org_id = $1 AND id = $2;

-- name: ListEndpointsByConsumer :many
SELECT id, org_id, consumer_id, url, description, event_types, secret_ciphertext, enabled, created_at, updated_at
FROM endpoints WHERE org_id = $1 AND consumer_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3;

-- name: ListEndpointsByConsumerAfter :many
SELECT id, org_id, consumer_id, url, description, event_types, secret_ciphertext, enabled, created_at, updated_at
FROM endpoints WHERE org_id = $1 AND consumer_id = $2 AND (created_at, id) < (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::uuid)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: UpdateEndpoint :execrows
UPDATE endpoints SET url = $3, description = $4, event_types = $5, enabled = $6, updated_at = now()
WHERE org_id = $1 AND id = $2;

-- name: UpdateEndpointSecret :execrows
UPDATE endpoints SET secret_ciphertext = $3, updated_at = now() WHERE org_id = $1 AND id = $2;

-- name: DeleteEndpoint :execrows
DELETE FROM endpoints WHERE org_id = $1 AND id = $2;
