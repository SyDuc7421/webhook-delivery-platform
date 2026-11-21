# ADR-0003: Kafka retry tiers for delayed retries

- Status: accepted · Date: 2026-11-18

## Context

A failed delivery must be retried later (1 min, 10 min, 1 h, 6 h), and the pending retry must survive
any process crash. Two questions follow: where pending retries are stored, and what happens to
per-endpoint ordering.

Messages to one endpoint share a Kafka partition. If a failing message is retried in place, every
later message in that partition waits behind it, including messages for other, healthy endpoints
(head-of-line blocking).

## Options

| Option | How it works | For | Against |
|---|---|---|---|
| Retry in place (block the partition) | Worker sleeps and retries before moving on | Strict order per endpoint | One broken endpoint stalls every endpoint on its partition for hours |
| Redis sorted set | `ZADD retries <next_attempt_at> <message_id>`; workers poll `ZRANGEBYSCORE … now` | Any delay per message (exponential backoff + jitter); one structure | Redis becomes durable pipeline state (needs AOF, careful failover); a worker crash between pop and send needs its own recovery; delivery state split across Kafka, Redis and Postgres |
| Postgres poller | `messages.next_attempt_at`; workers `SELECT … FOR UPDATE SKIP LOCKED` | Simplest and most durable; state already in Postgres | Constant polling load on the primary database at high volume |
| **Kafka retry topics** | `deliveries.retry.1m` → `.10m` → `.1h` → `.6h` → `deliveries.dlq`; the consumer pauses each partition until a record is due | Same durability and scaling model as the rest of the pipeline; healthy endpoints are unaffected | Fixed delays only; more topics; pause/resume logic; a retried message can arrive after later messages |

## Decision

Kafka retry topics. First attempts stay ordered per endpoint; retries are not ordered.

Redis is kept for shared, loss-tolerant state only (rate limits, caches); see
[architect.md §5](../architect.md#5-redis).

## Consequences

Documented for receivers: use `webhook-timestamp` and their own state to handle out-of-order events.
Retry-tier depth and DLQ count are monitored. If fixed delays prove too coarse, the Postgres poller is
the fallback, not Redis.
