# Observability

Stack: every Go process → stdout slog JSON → **Alloy** → **Loki** (logs); **Prometheus** scrapes
`/metrics` on `METRICS_ADDR` (:9100); OTel traces → Alloy → **Tempo**; **Grafana** on top.
Config lives in `deploy/observability/` and is provisioned as code (NFR-O4).

## Logs

- `log/slog` with a JSON handler, written to stdout.
- Mandatory fields on every line: `time`, `level`, `msg`, `service` (process name), `env`,
  `version`. Add `trace_id` + `span_id` whenever a span is active (NFR-O3), `request_id` on HTTP,
  `org_id` when authenticated, `event_id` / `message_id` / `endpoint_id` in the pipeline.
- Log at boundaries: request in/out, record consumed/produced, outbound attempt, state change.
  Not inside hot loops.
- Levels: `Debug` (local), `Info` (state change), `Warn` (degraded but handled, e.g. Redis down),
  `Error` (needs a human). A failed delivery attempt is **not** an `Error` — it is normal; log
  at `Info` with the status code.
- **Never** log API keys, signing secrets, JWTs, refresh tokens, passwords, `Authorization`
  headers, or event payload / response bodies (NFR-S5). Attempt records store a truncated
  response body in Postgres, not in logs.

Loki labels stay **low cardinality**: `service`, `env`, `level`, `container`. Never `org_id`,
`endpoint_id`, `trace_id` as labels — put them in the line and filter with `| json`.

```logql
{service="delivery-worker", level="error"} | json
{service=~"api|relay|dispatcher|delivery-worker|retry-worker"} | json | event_id="01J…"
```

## Metrics (Prometheus)

Pipeline metrics required by NFR-O2:

| Metric | Type | Labels |
| --- | --- | --- |
| `webhook_events_ingested_total` | counter | result (`accepted`/`duplicate`/`rejected`/`rate_limited`) |
| `webhook_outbox_backlog` | gauge | — (also updated while Kafka is down, so the backlog is visible exactly then) |
| `webhook_rate_limit_fail_open_total` | counter | — (requests let through because Redis was unavailable, NFR-D1) |
| `webhook_kafka_records_produced_total` | counter | topic, result |
| `webhook_kafka_records_consumed_total` | counter | topic, result |
| `webhook_kafka_consumer_lag` | gauge | group, topic (retry-tier depth = lag on `deliveries.retry.*`) |
| `webhook_delivery_attempts_total` | counter | tier (`first`/`1m`/`10m`/`1h`/`6h`), result (`success`/`http_error`/`timeout`/`blocked`) |
| `webhook_delivery_attempt_duration_seconds` | histogram | tier |
| `webhook_messages_dead_total` | counter | — |
| `webhook_end_to_end_seconds` | histogram | — (accepted → first success, for k6 NFR-P1) |

Platform metrics: `http_requests_total` / `http_request_duration_seconds` (method,
route **template**, status), `db_query_duration_seconds` (query_name),
`db_pool_connections` (state), `cache_operations_total` (op, result hit/miss/error).

Never put an id, URL, email or free text in a label. `endpoint_id` is **not** a label — use
exemplars / traces for per-endpoint drill-down.

## Traces (NFR-O1)

One trace per event: `POST /v1/events` → outbox row (store `traceparent` in the row) → relay
produce → dispatcher → delivery/retry worker → outbound HTTP. Context is propagated in HTTP
headers and in **Kafka record headers** (`traceparent`, `tracestate`). Span per request, DB
query, cache op, produce, consume, outbound HTTP. Span attributes follow the same secret rules as
logs. Sample 100% locally.

## Health

- `/healthz` — liveness. 200 unless the process is unrecoverable. No dependency checks.
- `/readyz` — readiness. Checks the dependencies this process actually uses (Postgres, Kafka,
  Redis) with a 2 s timeout. Redis down is reported but does **not** fail readiness (NFR-D1).

## Dashboards and alerts

- Dashboards: JSON in `deploy/observability/grafana/dashboards/`.
- Alerts: `deploy/observability/rules/alerts.yml` — outbox backlog growing, consumer lag growing,
  dead messages, low delivery success rate, API 5xx ratio.
- No latency targets before the first k6 baseline (spec §4.3); thresholds are revisited after M5.
- Every alert has a `runbook` annotation pointing at a section of `docs/RUNBOOK.md`. An alert
  without a runbook entry gets deleted.
