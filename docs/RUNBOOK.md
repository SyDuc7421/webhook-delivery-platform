# Runbook

> Symptom → check → fix. There is no always-on environment (ADR-0004): this covers local runs
> and AWS demo sessions. Alert rules in `deploy/observability/rules/alerts.yml` link here.

## 0. Triage order

1. Which stage? Grafana pipeline dashboard: ingest rate → outbox backlog → consumer lag per
   group → attempt success rate → dead messages.
2. Is it a dependency? `/readyz` of each process; Postgres, Kafka, Redis containers/services.
3. Did something change? Last deploy / last `terraform apply` / last image tag.
4. Blast radius? One endpoint (normal — its retries are isolated), one org, or everything.
5. Follow one event end to end: Loki `| json | event_id="…"` → `trace_id` → Tempo.

## 1. API 5xx rate

Check: `{service="api", level="error"} | json` grouped by `error.code`.
Common causes: DB pool exhausted (`db_pool_connections`, long transaction in
`pg_stat_activity`); bad deploy → roll back the image tag. Redis down must **not** cause 5xx
(NFR-D1) — if it does, the degradation path is broken: file a bug.

## 2. Outbox backlog growing

Ingest keeps working; deliveries are delayed. Check relay is running and its logs; check Kafka
reachability from the relay (`KAFKA_BROKERS`, security group on AWS). Fix the relay or Kafka;
the backlog drains by itself. Never delete outbox rows by hand.

## 3. Consumer lag growing

- `group=dispatcher` / `delivery-worker`: workers down or slow. Scale replicas (max useful =
  partitions); check `webhook_delivery_attempt_duration_seconds` — many 15 s timeouts mean many
  slow endpoints.
- `group=retry-worker.*`: expected to hold records until due. Only a problem if lag grows
  beyond the tier's normal volume, or if the paused partition never resumes (bug in pause/resume).
- A crash loop on one record (poison message): find the `message_id` in logs; the consumer must
  forward it to the next tier/DLQ instead of failing — fix the code, do not skip offsets by hand.

## 4. DLQ non-empty

Messages exhausted every tier (`status = 'dead'`). Usually the receiver is down or rejecting.
Inspect attempts for the endpoint in the dashboard. Once the receiver is fixed, replay from the
dashboard or `POST /v1/messages/{id}/replay` (same `webhook-id`, FR-R1). Bulk replay is a
stretch feature; do not script mass replays without checking the receiver dedupes.

## 5. Delivery success rate low

Group `webhook_delivery_attempts_total` by `result`:
`blocked` → SSRF guard rejecting (expected for private targets; check allowlist config for the
test receiver); `timeout` → slow receivers; `http_error` → receiver problem; all endpoints at
once → our egress (NAT, DNS) is broken.

## 6. Kafka broker down (single broker on EC2 / compose)

Ingest still accepts (outbox). Relay and workers fail health checks. Local: `make logs
svc=kafka`, `make up`. AWS: check the EC2 instance and EBS volume from the `kafka` module. After
recovery, consumers resume from committed offsets; duplicates are expected (at-least-once).

## 7. Database

- **Connections exhausted:** find idle-in-transaction sessions, terminate them, fix the leak.
- **Locks:** `pg_locks` join `pg_stat_activity`; identify the blocker before killing it.
- **Dirty migration:** `make migrate-force version=<last_good>` → repair SQL → re-run.
- **`delivery_attempts` insert fails with "no partition":** the next month's partition was not
  created — create it, then fix the creation job.

## 8. AWS demo session

Follow [deploy.md §4](deploy.md#4-demo-session-runbook-outline). Rollback = re-run `deploy`
with the previous image tag. Always finish with `destroy` and confirm in the console that
nothing is left running; the Budgets alert is the backstop (NFR-C2).
