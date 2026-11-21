# ADR-0007: Add Redis caches only after a measured baseline

- Status: accepted · Date: 2026-11-18

## Context

Two lookups run on every unit of work: API key → org on every ingest request, and consumer →
subscribed endpoints on every event the dispatcher handles. Both are read-heavy and change rarely,
so they are natural cache candidates. Caching also adds invalidation risk: a revoked key or a disabled
endpoint must not keep working because of a stale entry.

## Options

| Option | For | Against |
|---|---|---|
| Cache from the start | Fast from day one | No evidence the cache helps; invalidation bugs arrive before there are tests for them |
| No cache | Simplest | Leaves an obvious optimisation on the table |
| **Baseline first, then cache, then measure again** | The README can show the effect with real numbers; invalidation is built once the flows are stable | Caching work moves to M5 |

## Decision

Cache-aside in Redis for both lookups, added in M5 after a k6 baseline. Entries are deleted on every
write to the underlying rows; TTL ≤ 60 s bounds any race. On Redis errors, reads fall back to Postgres.

## Consequences

`docs/results/` holds two runs of the same k6 scenario (before and after). Integration tests cover
NFR-P3 (revocation and endpoint changes take effect on the next request).
