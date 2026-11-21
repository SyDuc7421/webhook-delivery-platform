# Kafka — delivery pipeline conventions

The *design* (processes, flow, why) is in [architect.md §3](architect.md#3-from-outbox-to-delivery)
and ADR-0002 / ADR-0003. This file is the *rules* every producer and consumer follows.

## Topics

Created by `kafka-init` in `deploy/docker-compose.yml` locally (`make topics`) and by the `kafka`
Terraform module on AWS. Auto-create is off: a topic that is not declared does not exist.
Topic names live only in `internal/messaging/kafka/topics.go`.

| Topic | Key | Producer | Consumer group |
| --- | --- | --- | --- |
| `events` | `consumer_uid` | relay | `dispatcher` |
| `deliveries` | `endpoint_id` | dispatcher, replay | `delivery-worker` |
| `deliveries.retry.1m` / `.10m` / `.1h` / `.6h` | `endpoint_id` | delivery-worker, retry-worker | `retry-worker.<tier>` |
| `deliveries.dlq` | `endpoint_id` | retry-worker | — (status lives in Postgres) |

- The key decides ordering. `deliveries` is keyed by `endpoint_id` so first attempts to one
  endpoint stay ordered (FR-D5). Never change a key without an ADR.
- Partition count is fixed at creation (local default 6). Increasing it reshuffles keys and
  breaks ordering for in-flight messages — needs an ADR.
- Retention on `events` / `deliveries` must exceed the longest retry tier (6 h) plus slack; the
  source of truth is still Postgres.

## Record shape

Value: JSON of an explicit DTO from `internal/messaging/kafka`, never a domain struct.

```json
// events
{ "event_id": "01J…", "org_id": "…", "consumer_uid": "shop_42", "type": "payment.paid", "accepted_at": "…" }

// deliveries, deliveries.retry.*, deliveries.dlq
{ "message_id": "01J…", "event_id": "01J…", "endpoint_id": "…", "attempt": 1 }
```

- Records carry **ids, not payloads**. The worker loads the payload and endpoint (URL, secret,
  enabled flag) from Postgres, so a disabled endpoint or rotated secret applies immediately
  (FR-E4, FR-E3) and secrets never sit in Kafka.
- Headers: `traceparent` / `tracestate` (W3C, NFR-O1), `schema-version`, and on retry topics
  `retry-tier`. Record timestamp = when the record was produced to that topic; retry delay is
  measured from it.
- Additive changes only. A breaking change = new `schema-version` handled side by side.

## Producing

- **Never produce inside the ingest request or inside a DB transaction.** Ingest writes
  `events` + `outbox` in one transaction; only the relay produces (ADR-0002).
- Producers use `acks=all` and idempotent production; wait for the ack before deleting outbox
  rows or committing a consumer offset.
- Produce errors are returned, not logged-and-dropped. The relay leaves the row and retries.

## Consuming — at-least-once

Every consumer loop follows the same order:

1. Fetch a batch. Per-record `context.WithTimeout`.
2. Do the work **through a service** (consumers never call repositories directly).
3. Make the result durable: DB write committed **and** any follow-up record produced + acked.
4. Only then commit the offset.

A crash between 3 and 4 replays the record. Therefore every consumer is **idempotent**:

| Consumer | Idempotency guard |
| --- | --- |
| dispatcher | `messages` unique `(event_id, endpoint_id)` → `INSERT … ON CONFLICT DO NOTHING`; re-produce is harmless |
| delivery-worker / retry-worker | a duplicate send is allowed (at-least-once); receivers dedupe on `webhook-id` = `message_id`. Skip if the message is already `succeeded` or `dead` and the record is not a replay |

Never block a partition on one failing message: a failure is forwarded to the next tier and
the offset moves on (NFR-R2).

## Retry tiers

`deliveries` → `.retry.1m` → `.retry.10m` → `.retry.1h` → `.retry.6h` → `deliveries.dlq`
(FR-D4, configurable via `RETRY_TIERS`).

- The retry-worker for a tier reads a record, computes `due = record.timestamp + tier delay`, and
  if `now < due` **pauses that partition** and resumes it at `due`. No `time.Sleep` in the poll
  loop, no busy loop.
- On failure, produce to the next tier with `attempt+1`, update `messages` (status, attempt
  count, next attempt time), then commit.
- After `.6h` fails: produce to `deliveries.dlq`, set `messages.status = 'dead'`, increment
  `webhook_messages_dead_total`.
- Retries are **not** ordered relative to later first attempts. Receivers use
  `webhook-timestamp` (documented for them; ADR-0003).

## Replay

`POST /v1/messages/{id}/replay` produces the existing message to `deliveries` with the same
`message_id` (same `webhook-id`, FR-R1). Works for `succeeded`, `failed` and `dead` (FR-R2). The
replay flag in the record lets the worker bypass the "already succeeded" skip.

## Shutdown (NFR-R3)

SIGTERM → stop fetching → finish in-flight records (bounded by `SHUTDOWN_TIMEOUT`) → commit what
is done → leave the group → close producers. Unfinished records are simply re-delivered after
rebalance.

## Metrics

`webhook_kafka_consumer_lag{group,topic}` (retry-tier depth = lag on retry topics),
`webhook_kafka_records_consumed_total{topic,result}`, `webhook_kafka_records_produced_total{topic,result}`,
`webhook_outbox_backlog`. See [OBSERVABILITY.md](OBSERVABILITY.md).

## Testing

Unit-test the service function with a fake producer. Integration-test against a real broker
(testcontainers) with unique topic names per test, asserting: happy path, duplicate record
(no second `messages` row), failure → next tier, last tier → DLQ + `dead`, offset not committed
when the DB write fails, and graceful shutdown mid-batch. The NFR-R1 failure test kills the
worker process mid-batch.
