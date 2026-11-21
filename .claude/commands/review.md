---
description: Pre-PR self review of the current diff
---

Run `git diff main...HEAD` and review it as a hostile senior reviewer. Check, in order:

**Correctness** — logic, off-by-one, nil handling, error paths actually reachable.
**Layering** — no handler importing a repository; no business rule in a handler or in SQL.
**Errors** — every `err` handled or wrapped with context; sentinel errors used; nothing both
logged and returned.
**Context** — `ctx` propagated to every I/O call; timeouts set on external calls.
**Data** — parameterised SQL; index exists for every new query pattern; no N+1; transaction
scope minimal and free of network calls.
**Concurrency** — every goroutine has an owner and a stop condition; no data race.
**Pipeline** — no Kafka produce in the ingest request or inside a tx; offset committed only after
the DB write and follow-up produce are durable; consumer idempotent on replayed records; failures
forwarded to the next tier, never retried in place; trace context propagated in record headers.
**Redis** — every key has a TTL and is built in `keys.go`; Redis errors degrade (fail open / fall
back to Postgres); cache entries deleted on write; nothing durable stored in Redis.
**Security** — org scoping (`org_id` from auth, never from input) on every query; no API key,
signing secret, token, or payload in logs/traces; outbound HTTP only via `internal/webhook`
(SSRF guard); input validated at the boundary.
**Observability** — logs at boundaries, metrics for new external calls, no high-cardinality
labels.
**Tests** — error and edge paths covered; no assertion weakened to make a test pass.
**Hygiene** — no unrelated reformatting, no dead code, no TODO without an issue, docs,
`.env.example` and the FR/NFR status in `docs/spec.md` updated.

Output: a table of `file:line | severity (blocker/major/nit) | issue | suggested fix`. Then fix
the blockers and majors, leave the nits listed.
