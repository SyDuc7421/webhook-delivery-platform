# Architecture — webhook-platform

How the pieces fit together. For *what* the system must do, see [spec.md](spec.md).
Decisions and their trade-offs are recorded in [adr/](adr/).

---

## 1. The big picture

```
 org backend
     │  POST /v1/events  (API key, Idempotency-Key)
     ▼
┌──────────┐   one transaction   ┌────────────────────────────┐
│   api    │ ──────────────────▶ │ PostgreSQL                 │
└──────────┘  events + outbox    │ events, outbox, messages,  │
                                 │ delivery_attempts, ...     │
                                 └──────────┬─────────────────┘
                       FOR UPDATE SKIP LOCKED│
                                 ┌──────────▼─────────┐
                                 │       relay        │
                                 └──────────┬─────────┘
                                 Kafka `events` (key = consumer_uid)
                                 ┌──────────▼─────────┐
                                 │     dispatcher     │  fan-out to subscribed endpoints
                                 └──────────┬─────────┘
                                 Kafka `deliveries` (key = endpoint_id)
                                 ┌──────────▼─────────┐   signed HTTP POST
                                 │  delivery-worker   │ ─────────────────▶ customer endpoint
                                 └──────────┬─────────┘
                          failure           │
                                 ┌──────────▼─────────┐
                                 │    retry-worker    │  retry.1m → .10m → .1h → .6h → dlq
                                 └────────────────────┘
```

### Processes

| Process | Does | Talks to |
|---|---|---|
| `api` | Dashboard REST API, ingest API, test receiver | Postgres, Redis |
| `relay` | Moves outbox rows to Kafka | Postgres, Kafka |
| `dispatcher` | Turns one event into one message per subscribed endpoint | Kafka, Postgres, Redis (cache, from M5) |
| `delivery-worker` | Sends first attempts | Kafka, Postgres, the internet |
| `retry-worker` | Sends retries per tier, moves exhausted messages to the DLQ | Kafka, Postgres, the internet |
| `web` | Next.js dashboard | `api` |

Each process is its own binary (`backend/cmd/<process>`, one image per process via the
`APP` build arg); shared wiring lives in `backend/cmd/internal/boot`. Every process serves
`/metrics`, `/healthz` and `/readyz` on `METRICS_ADDR` (:9100).
Inside each process the code follows the layering in [§9](#9-code-layering-inside-a-process).

### Backing services

| Service | Why it exists |
|---|---|
| **PostgreSQL** | Source of truth: orgs, endpoints, events, messages, attempts; the outbox |
| **Kafka** | Ordered, replayable, horizontally scalable delivery pipeline |
| **Redis** | Shared, short-lived state: rate limits, refresh tokens, lookup caches (see §5) |
| **Prometheus / Loki / Tempo / Grafana** (+ Alloy collector) | Metrics, logs, traces, dashboards and alerts |

---

## 2. Ingest (the write path)

```
POST /v1/events
  ├─ authenticate API key (hash lookup → org; Redis cache-aside from M5)
  ├─ rate limit per org (Redis)
  ├─ validate: JSON, ≤ 256 KB, consumer exists, type exists
  ├─ BEGIN
  │    INSERT events … ON CONFLICT (org_id, idempotency_key) DO NOTHING
  │    INSERT outbox (event_id)          ← only if the event is new
  │  COMMIT
  └─ 202 Accepted { id }                 ← repeat key → the original event
```

Kafka is not touched in the request. If Kafka is down, events wait in the outbox. See
[ADR-0002](adr/0002-transactional-outbox.md).

## 3. From outbox to delivery

1. **relay** locks a batch: `SELECT … FROM outbox ORDER BY id LIMIT n FOR UPDATE SKIP LOCKED`,
   produces each event to `events` keyed by `consumer_uid`, waits for broker acks, deletes the rows,
   commits. Several relays can run side by side.
2. **dispatcher** consumes `events`, finds enabled endpoints of that consumer subscribed to the type,
   inserts one `messages` row per endpoint, produces each to `deliveries` keyed by `endpoint_id`,
   then commits its offset.
3. **delivery-worker** consumes `deliveries`, signs and sends the request, writes a
   `delivery_attempts` row, then either marks the message `succeeded` or produces it to
   `deliveries.retry.1m`, and only then commits the offset.
4. **retry-worker** consumes each tier topic. For each record it waits until
   `record timestamp + tier delay` by pausing the partition (no busy loop), retries, and forwards to
   the next tier on failure. After `.6h` the message goes to `deliveries.dlq` and is marked `dead`.

### Kafka topics

| Topic | Key | Producer | Consumer group |
|---|---|---|---|
| `events` | `consumer_uid` | relay | `dispatcher` |
| `deliveries` | `endpoint_id` | dispatcher, replay | `delivery-worker` |
| `deliveries.retry.1m` / `.10m` / `.1h` / `.6h` | `endpoint_id` | delivery-worker, retry-worker | `retry-worker.<tier>` |
| `deliveries.dlq` | `endpoint_id` | retry-worker | — (dashboard reads Postgres status) |

Rules for producers and consumers: [MESSAGING.md](MESSAGING.md).

### Delivery semantics

- **At-least-once.** Offsets are committed only after the attempt is recorded and the message is
  forwarded. A crash in between means a duplicate send; receivers dedupe on `webhook-id`.
- **Ordering.** First attempts to one endpoint are ordered (same partition). Retries are not: a failing
  message moves to a retry topic so it does not block the partition. See
  [ADR-0003](adr/0003-retry-tiers-over-strict-ordering.md).
- **Replay.** Produces the existing message to `deliveries` again; same `webhook-id`, new attempt.

---

## 4. PostgreSQL design

| Table | Notes |
|---|---|
| `orgs`, `users`, `api_keys` | `api_keys` holds hash + prefix only |
| `event_types`, `consumers`, `endpoints` | unique `(org_id, name)` / `(org_id, uid)` |
| `events` | `payload JSONB`; unique `(org_id, idempotency_key)`; GIN index on `payload` |
| `outbox` | `id`, `event_id`; drained by relay |
| `messages` | event × endpoint; status, attempt count, next attempt time |
| `delivery_attempts` | **declaratively partitioned by month**; retention = drop old partitions |

- All list queries use **keyset (cursor) pagination**.
- The event-log query is profiled with `EXPLAIN ANALYZE` before and after indexing; results go in
  `docs/results/`.

---

## 5. Redis

Redis holds only state that is shared between instances and safe to lose. Nothing in the delivery
pipeline depends on Redis for durability: retries live in Kafka, delivery status lives in Postgres
(see [ADR-0003](adr/0003-retry-tiers-over-strict-ordering.md)).

| Use | Key | Pattern | TTL | Milestone |
|---|---|---|---|---|
| Ingest rate limit per org | `rl:org:{org_id}` | fixed or sliding window counter, shared by all `api` instances | window length | M1 |
| Refresh tokens | `rt:{token_hash}` | single use via `GETDEL` (as in `shorten-link`) | `JWT_REFRESH_TTL` | M1 |
| API key → org | `ak:{key_hash}` | cache-aside, deleted on revoke | ≤ 60 s | M5 |
| Consumer → subscribed endpoints | `subs:{consumer_id}` | cache-aside, deleted on any endpoint write for that consumer | ≤ 60 s | M5 |
| Per-endpoint send rate limit | `rl:ep:{endpoint_id}` | token bucket (Lua script), shared by all workers | bucket refill | stretch |
| Circuit breaker state | `cb:{endpoint_id}` | open/half-open/closed with expiry, shared by all workers | open period | stretch |

Rules (TTLs, invalidation, fail-open): [CACHING.md](CACHING.md).

**Caches are added after a baseline.** M5 first runs k6 without caches, then adds the two caches and
runs the same scenario again; both results are committed to `docs/results/`. See
[ADR-0007](adr/0007-cache-after-measuring.md).

**If Redis is down:** rate limiting fails open (requests are accepted and the event is logged),
caches fall back to Postgres, refresh fails (users log in again). Delivery keeps working.

---

## 6. Security mechanics

- **Signing**: HMAC-SHA256 over `{webhook-id}.{webhook-timestamp}.{body}` with the endpoint secret,
  following the Standard Webhooks header convention (verify against the spec when implementing).
  The docs include a verification snippet for receivers.
- **SSRF guard**: a custom `net.Dialer` `Control`/`DialContext` rejects private, loopback,
  link-local, CGNAT and metadata addresses after DNS resolution, so DNS rebinding cannot bypass it.
  Only the test receiver host is allowlisted, by config.
- **Outbound client**: 15 s timeout, no redirects, response body read capped.

---

## 7. Observability

- OpenTelemetry SDK in every Go process; trace context travels in **Kafka record headers**, so one
  trace spans api → relay → dispatcher → worker → outbound HTTP.
- Prometheus metrics: ingest rate, success rate, attempt latency (p50/p95/p99), retry-tier depth,
  DLQ count, consumer lag per group, outbox backlog.
- Logs: structured JSON (`log/slog`, [ADR-0009](adr/0009-backend-stack-chi-slog.md)) with `trace_id`, shipped to Loki by Alloy.
- Grafana dashboards and alert rules are provisioned from files in `/deploy/observability`.

---

## 8. Repository layout

```
/backend    Go module: cmd/*, internal/*, migrations
/web        Next.js dashboard
/infra      Terraform (see deploy.md)
/deploy     docker-compose.yml, observability/ (Grafana/Prometheus/Loki/Tempo/Alloy config)
/loadtest   k6 scripts
/docs       spec, architecture, deploy, testing, roadmap, conventions, ADRs, results
```

Monorepo: see [ADR-0006](adr/0006-monorepo.md).

---

## 9. Code layering inside a process


### Shape

One Go module (`backend/`), five processes sharing the same `internal/` packages, one database.
The only rule that matters is the dependency direction.

```
            ┌────────────── inbound ───────────────────┐
HTTP (chi)  ┤ handler → DTO → validate → map           │   api
Kafka       ┤ consumer loop → record DTO → map         │   dispatcher, delivery-worker, retry-worker
Ticker      ┘ outbox poll                              │   relay
                        ↓
                    service (use case)
         ingest · dispatch · deliver · retry · replay · CRUD
          orchestration · transactions · org scoping · policy
                        ↓
                     domain
        entities · value objects · sentinel errors
   interfaces (Repository, Cache, RateLimiter, Producer, Sender, Clock)
                        ↑ implemented by
   postgres (pgx)   redis   kafka producers   webhook (signing + SSRF-safe client)
```

- `domain` imports nothing from `internal/`.
- `service` imports `domain` only.
- Adapters (`http`, `repository`, `cache`, `messaging`, `webhook`) import `domain` (+ `service`
  for inbound adapters).
- `cmd/<process>/main.go` is the only place that knows every package. Manual constructor
  wiring — no DI container. Each process wires only what it uses.

### Request lifecycle (api)

`recover → requestID → logger → tracing → metrics → CORS → auth (JWT or API key) → rateLimit
(ingest) → body limit 256 KB → decode + validate → handler → service → repo/cache → response
envelope`

### Record lifecycle (workers)

`fetch → extract trace context from headers → per-record timeout → service → DB commit +
produce follow-up (acked) → commit offset` — see [MESSAGING.md](MESSAGING.md).

### Cross-cutting decisions

| Concern | Decision |
| --- | --- |
| Config | env vars → typed struct, validated at boot, fail fast |
| Errors | sentinel errors in `domain`, single HTTP mapping table |
| Transactions | owned by service layer via `Tx(ctx, fn)`; repos accept a `Querier` |
| Async | Kafka; transactional outbox for DB↔broker atomicity (ADR-0002) |
| Retries | Kafka retry tiers + DLQ (ADR-0003) |
| Idempotency | Postgres unique constraints (`Idempotency-Key`, `(event_id, endpoint_id)`); receivers dedupe on `webhook-id` |
| Cache | cache-aside, TTL ≤ 60 s, delete on write, only after a baseline (ADR-0007) |
| Observability | slog JSON → Alloy → Loki; Prometheus; OTel → Tempo |
| Shutdown | SIGTERM → stop accepting/fetching → drain in-flight (`SHUTDOWN_TIMEOUT`, 15 s) → commit → close pools |

### Scaling notes

Every process is stateless and scales horizontally: `api` behind the ALB, `relay` via
`SKIP LOCKED`, workers via consumer groups (max useful replicas = partition count). State lives
in Postgres (source of truth), Kafka (in-flight work) and Redis (loss-tolerant only).

### Not included on purpose

An ORM, a DI framework, gRPC, Kubernetes manifests, exactly-once delivery, multi-region. Add one
only with an ADR.
