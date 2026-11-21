# PostgreSQL

Driver: `pgx/v5` (stdlib `database/sql` only if a tool demands it). Queries: hand-written SQL or
`sqlc`. No ORM.

## Migrations — golang-migrate

```bash
make migrate-new name=add_events_table   # creates backend/migrations/000123_add_events_table.{up,down}.sql
make migrate-up
make migrate-down                        # local only
```

Rules:
- Append-only. An applied migration is immutable — fix forward.
- Every `up` has a real `down`. If it cannot be reversed, say so in a comment and document the
  manual recovery path.
- One logical change per migration.
- **Zero-downtime discipline:** add nullable column → backfill in batches → add constraint →
  switch code → drop old column in a later release. Never rename in place.
- `CREATE INDEX CONCURRENTLY` for tables > ~100k rows (and no transaction wrapper).
- Destructive DDL (`DROP`, `ALTER TYPE`) needs an ADR + explicit approval.

## Schema conventions

- Tables `snake_case` plural; columns `snake_case`.
- PK `id uuid PRIMARY KEY` (UUIDv7 generated in app).
- Every table: `created_at timestamptz NOT NULL DEFAULT now()`,
  `updated_at timestamptz NOT NULL DEFAULT now()`; soft delete only when required
  (`deleted_at timestamptz`), and then every query must filter it.
- FKs always declared with an explicit `ON DELETE` action. Index every FK column.
- `NOT NULL` by default; nullable is a deliberate decision.
- Every tenant-owned table has `org_id uuid NOT NULL`, and every unique constraint and hot index
  starts with `org_id` (`(org_id, name)`, `(org_id, uid)`, `(org_id, idempotency_key)`).
- Payloads: `jsonb`. GIN index on `events.payload` only once a query needs it.
- Enums: `text` + `CHECK` constraint (cheaper to evolve than a PG enum type), e.g.
  `messages.status IN ('pending','succeeded','failed','dead')`.

## Tables with special rules

| Table | Rule |
| --- | --- |
| `outbox` | Written in the same transaction as `events`. Drained by the relay with `SELECT … ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`; rows deleted after the Kafka ack. Never read by anything else. |
| `messages` | Unique `(event_id, endpoint_id)` so a replayed `events` record cannot fan out twice. |
| `delivery_attempts` | **Declaratively partitioned by month** on `created_at`. Retention = drop old partitions, never `DELETE`. Future partitions are created ahead of time (mechanism decided in M2). |
| `api_keys` | Hash + visible prefix only. Lookup by hash. |
| `endpoints` | Signing secret never selected by list queries. |

## Pagination

Keyset only: `WHERE org_id = $1 AND (created_at, id) < ($2, $3) ORDER BY created_at DESC, id DESC
LIMIT $4`, backed by an index on `(org_id, created_at DESC, id DESC)` (plus the filter columns
for the event log). Profile the event-log query with `EXPLAIN (ANALYZE, BUFFERS)` before and after
indexing; commit results to `docs/results/`.

## Queries

- Parameterised always: `$1, $2`. String concatenation is a security bug.
- `SELECT` explicit columns. Never `SELECT *` in application code.
- Every new query pattern must have a supporting index — attach the `EXPLAIN (ANALYZE, BUFFERS)`
  output to the PR for anything on a hot path.
- Batch reads to kill N+1 (`WHERE id = ANY($1)`), e.g. loading endpoints for a fan-out.
- Statement timeout set per pool. There is no read replica: the event-log query must stay
  index-backed and paginated, never a full scan in a request.

## Transactions

Owned by the **service** layer. Repos accept a `Querier` (satisfied by both pool and tx).

```go
err := s.tx.Do(ctx, func(ctx context.Context, q postgres.Querier) error {
    created, err := s.events.Insert(ctx, q, event)   // ON CONFLICT (org_id, idempotency_key) DO NOTHING
    if err != nil || !created { return err }
    return s.outbox.Enqueue(ctx, q, event.ID)       // event + outbox row, atomically (ADR-0002)
})
```

- Keep transactions short. No HTTP calls, no Kafka produce, no sleeps inside a tx.
  (The relay is the one exception: it holds the `SKIP LOCKED` row locks while waiting for the
  Kafka ack, with a bounded batch and timeout.)
- Default isolation `READ COMMITTED`; raise it deliberately and document why.
- Concurrency: optimistic locking with a `version int` column, or `SELECT … FOR UPDATE` when
  contention is real. Always lock rows in a consistent order.

## Pool

`max_conns` sized to `(cores * 2) + effective_spindles`, capped so that
`replicas * max_conns < postgres max_connections * 0.8`. Set `max_conn_lifetime` and
`health_check_period`.

## Testing

Integration tests run against a real Postgres via testcontainers (or the compose stack).
Never mock the database — mock the repository interface for unit tests instead.
