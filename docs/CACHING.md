# Redis — conventions

Redis holds only state that is **shared between instances and safe to lose**. Nothing in the
delivery pipeline depends on Redis for durability: retries live in Kafka, delivery status lives
in Postgres (ADR-0003). If flushing Redis breaks correctness, the design is wrong.

What is stored and why: [architect.md §5](architect.md#5-redis). This file is the rules.

## Keys

The key list (use, pattern, TTL, milestone) is in [architect.md §5](architect.md#5-redis).

- Keys are constructed **only** by helpers in `internal/cache/redis/keys.go`. No inline strings.
- A value format change = new key prefix (e.g. `ak2:`), not a migration.
- Never store a raw API key, refresh token or signing secret — keys use hashes.

## Rules

- **Every key has a TTL.** No exceptions.
- **Caches are added only after a k6 baseline** (ADR-0007, NFR-P2). Do not add a new cache
  without numbers that show it is needed.
- Cache-aside: read → miss → load from Postgres → set. The write path **deletes** the entry
  (`DEL`) after commit; it never "updates" the cache. TTL ≤ 60 s bounds any race (NFR-P3).
  - API key revoked → `DEL ak:{hash}`.
  - Any endpoint create/update/delete/disable for a consumer → `DEL subs:{consumer_id}`.
- **Degrade, never fail (NFR-D1):** on any Redis error, log a `Warn`, increment
  `cache_operations_total{result="error"}`, and
  - rate limit → **fail open** (accept the request);
  - cache → read from Postgres;
  - refresh token → reject (user logs in again).
- No `KEYS` — `SCAN` with a cursor if you must iterate.
- Values are JSON of an explicit DTO, never a domain struct.

## Rate limiting

Per-org fixed or sliding window counter shared by every `api` instance (`INCR` + `EXPIRE` in one
Lua script, or `MULTI`). Over the limit → `429` with `Retry-After` (FR-I5). The stretch
per-endpoint send limit is a token bucket in Lua.

## Not in Redis

Idempotency keys (`Idempotency-Key` → Postgres unique `(org_id, idempotency_key)`), consumer
dedupe (Postgres unique constraints), pending retries (Kafka tiers), locks (Postgres
`FOR UPDATE SKIP LOCKED`).
