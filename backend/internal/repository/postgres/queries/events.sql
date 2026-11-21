-- name: InsertEvent :execrows
INSERT INTO events (id, org_id, consumer_id, event_type_id, consumer_uid, type, payload, idempotency_key, traceparent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (org_id, idempotency_key) DO NOTHING;

-- name: GetEventByIdempotencyKey :one
SELECT id, org_id, consumer_id, event_type_id, consumer_uid, type, payload, idempotency_key, traceparent, created_at
FROM events WHERE org_id = $1 AND idempotency_key = $2;

-- name: EnqueueOutbox :exec
INSERT INTO outbox (event_id) VALUES ($1);

-- name: LockOutboxBatch :many
SELECT o.id AS outbox_id, e.id, e.org_id, e.consumer_id, e.event_type_id, e.consumer_uid, e.type, e.payload,
       e.idempotency_key, e.traceparent, e.created_at
FROM outbox o JOIN events e ON e.id = o.event_id
ORDER BY o.id LIMIT $1
FOR UPDATE OF o SKIP LOCKED;

-- name: DeleteOutboxRows :exec
DELETE FROM outbox WHERE id = ANY($1::bigint[]);

-- name: CountOutbox :one
SELECT count(*) FROM outbox;
