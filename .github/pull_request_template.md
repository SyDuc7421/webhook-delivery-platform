## What & why

<!-- One paragraph. Link the ticket. -->
Refs: #<issue> · Requirements: FR-… / NFR-… · Milestone: M…

## Type

- [ ] feat  - [ ] fix  - [ ] refactor  - [ ] perf  - [ ] docs  - [ ] chore  - [ ] breaking

## How it was tested

<!-- Commands run, cases covered, screenshots/logs if useful. -->

## Checklist

- [ ] `make check` passes
- [ ] Tests cover happy path **and** error/edge paths
- [ ] Layering respected (no handler → repository)
- [ ] Errors wrapped; nothing logged *and* returned
- [ ] No secrets or PII in code, logs, or fixtures
- [ ] Migration is reversible and zero-downtime (or an ADR explains why not)
- [ ] Index added for any new query pattern (`EXPLAIN` attached below if hot path)
- [ ] Consumers are idempotent; offsets committed after durable work; retry/DLQ path tested
- [ ] No Kafka produce in the ingest request; no outbound HTTP outside `internal/webhook`
- [ ] Metrics/logs/traces added for new external calls
- [ ] `.env.example`, `docs/`, OpenAPI updated; spec status column updated
- [ ] ADR added for a hard-to-reverse decision

## Risk & rollback

**Blast radius:** <!-- endpoint / topic / process / everything -->
**Rollback:** <!-- redeploy previous tag? does the old code tolerate the new schema? -->

## AI assistance

- [ ] Parts of this PR were AI-generated and I have reviewed every line.
