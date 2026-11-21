---
description: Scaffold a Kafka consumer loop with idempotency, retry tiers and DLQ
argument-hint: <topic> <group> — e.g. deliveries delivery-worker
---

Read `docs/MESSAGING.md` and `docs/ARCHITECT.md` §3 first. Build a consumer for `$ARGUMENTS`:

1. Topic name + consumer group constant in `backend/internal/messaging/kafka/topics.go`. A new
   topic also needs `kafka-init` in `deploy/docker-compose.yml`, the `kafka` Terraform module and
   an ADR — stop and ask before adding one.
2. Record DTO + `schema-version` header handling. Unknown version → forward to the DLQ path and
   log, never silently drop.
3. Extract trace context from record headers; per-record `context.WithTimeout`.
4. Call a **service**, never a repository. The service makes the result durable (DB commit and
   any follow-up record produced + acked) **before** the loop commits the offset.
5. Idempotency: state the guard (Postgres unique constraint, status check) and why a replayed
   record is a no-op or an allowed duplicate.
6. Failure handling: forward to the next retry tier / DLQ and move on — never block the
   partition, never retry in place. Retry-tier consumers pause the partition until due.
7. Metrics: `webhook_kafka_records_consumed_total{topic,result}`, lag gauge. Logs with
   `event_id` / `message_id`, no payloads or secrets.
8. Graceful shutdown: stop fetching, finish in-flight, commit, leave group, close.
9. Tests (testcontainers, unique topic per test): happy path, duplicate record, transient error →
   next tier, last tier → DLQ, DB failure → offset not committed, shutdown mid-batch.
