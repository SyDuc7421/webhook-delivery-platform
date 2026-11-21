# Backend code architecture

How code is layered **inside each Go process**. For the system view (processes, Kafka topics,
tables, Redis keys) see [architect.md](architect.md).

## Shape

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

## Request lifecycle (api)

`recover → requestID → logger → tracing → metrics → CORS → auth (JWT or API key) → rateLimit
(ingest) → body limit 256 KB → decode + validate → handler → service → repo/cache → response
envelope`

## Record lifecycle (workers)

`fetch → extract trace context from headers → per-record timeout → service → DB commit +
produce follow-up (acked) → commit offset` — see [MESSAGING.md](MESSAGING.md).

## Cross-cutting decisions

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

## Scaling notes

Every process is stateless and scales horizontally: `api` behind the ALB, `relay` via
`SKIP LOCKED`, workers via consumer groups (max useful replicas = partition count). State lives
in Postgres (source of truth), Kafka (in-flight work) and Redis (loss-tolerant only).

## Not included on purpose

An ORM, a DI framework, gRPC, Kubernetes manifests, exactly-once delivery, multi-region. Add one
only with an ADR.
