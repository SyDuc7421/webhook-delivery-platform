# AGENTS.md

> Single source of truth for **every** AI coding agent (Claude Code, Cursor, Copilot, Codex,
> Gemini CLI…). `CLAUDE.md` and `.github/copilot-instructions.md` are thin pointers to this
> file. Edit here, never fork the content.

## 1. Project snapshot

| Field | Value |
| --- | --- |
| Name | `webhook-platform` — Webhook Delivery Platform |
| Layout | Monorepo: `/backend` (Go), `/web` (Next.js), `/infra` (Terraform), `/deploy`, `/loadtest`, `/docs` |
| Go module | `github.com/SyDuc7421/webhook-delivery-platform/backend` |
| Runtime | Go 1.27 · `net/http` + chi · `log/slog` |
| Datastores | PostgreSQL 19 (source of truth, outbox) · Redis 8.10 (rate limits, refresh tokens, caches) |
| Messaging | Apache Kafka 4.4 (KRaft) — franz-go |
| Observability | OpenTelemetry · Prometheus · Loki · Tempo · Grafana (Alloy as collector) |
| Local env | Docker Compose (`deploy/docker-compose.yml`) |
| Cloud | AWS, ephemeral (Terraform apply/destroy), Free plan |

A small Svix/Hookdeck: an org calls `POST /v1/events`; the platform delivers the event as a signed
HTTP POST to every subscribed endpoint, retries failures through Kafka retry tiers, keeps
first-attempt order per endpoint, records every attempt and supports replay. At-least-once,
never exactly-once. Requirements: `docs/SPEC.md`. System design: `docs/ARCHITECT.md`.

## 2. Golden rules (non-negotiable)

1. **Read before write.** Inspect the existing implementation of a sibling feature before
   creating a new one. Match its shape; do not invent a second pattern.
2. **No new dependency without approval.** Ask first. Prefer stdlib → existing dep → new dep.
3. **Never edit generated files.** `*.gen.go`, `sqlc` output, `mocks/`.
   Change the source + regenerate (`make gen`).
4. **Never commit secrets.** `.env` is gitignored; only `.env.example` is tracked and it holds
   dummy values.
5. **Every change ships with tests.** No test → the change is not done. See `docs/TESTING.md`.
6. **`make check` must pass** before you say you are finished. If it fails, fix it — do not
   disable the linter, do not add `//nolint` without a reason comment.
7. **Migrations are append-only.** Never edit an applied migration; write a new one.
8. **Errors are wrapped, never swallowed.** `fmt.Errorf("doing x: %w", err)`. No bare `panic`
   outside `main()` bootstrap.
9. **`context.Context` is the first parameter** of every function that does I/O.
10. **Never lose an accepted event.** No Kafka produce inside the ingest request (outbox only);
    no offset commit before the attempt is recorded and the message forwarded.
11. **Requirements have IDs.** Reference `FR-*` / `NFR-*` from `docs/SPEC.md` in tests, PRs and
    commits; update the status column when a requirement is met.
12. **Ask, don't guess.** If the requirement is ambiguous, stop and ask one focused question.

## 3. Commands

Everything goes through the root `Makefile`. Go targets run inside `backend/`.

```bash
make setup        # one-time: install tools, git hooks, .env
make up           # docker compose up (postgres, kafka, redis, observability)
make down         # stop stack; make clean also drops volumes
make run          # run the api locally with live reload (air)
make check        # fmt + vet + lint + test — the pre-push gate
make test         # unit tests
make test-int     # integration tests (testcontainers, needs Docker)
make lint         # golangci-lint run
make fmt          # gofumpt + goimports
make gen          # sqlc / mocks / openapi codegen
make migrate-new name=add_events_table
make migrate-up   # / make migrate-down
make topics       # (re)create Kafka topics locally
make logs svc=api
```

## 4. Repository map

```
backend/
  cmd/<process>/main.go      Composition root per process (api, relay, dispatcher,
                             delivery-worker, retry-worker). Wiring only — zero business logic.
  cmd/internal/boot/         Shared wiring: config, logger, signals, ops server, dependency
                             constructors. Importable only from cmd/.
  internal/
    config/                  Env parsing + validation. Fail fast at boot.
    http/                    chi: router, middleware, handlers, DTOs, error mapping.
    domain/                  Entities + interfaces. Depends on NOTHING else in internal/.
    service/                 Use cases: ingest, dispatch, deliver, retry, replay, CRUD.
    repository/postgres/     pgx queries / sqlc output. Implements domain interfaces.
    cache/redis/             Rate limits, refresh tokens, lookup caches. Keys defined here only.
    messaging/kafka/         Topics, producers, consumers, retry tiers, header propagation.
    webhook/                 Outbound delivery: signing, SSRF-safe HTTP client.
    observability/           logger, tracer, metrics, health.
    platform/                Cross-cutting helpers (errs, pagination cursors, validator, ids).
  migrations/                golang-migrate SQL files. Append-only.
  test/                      Integration + failure tests, fixtures, testcontainers setup.
web/                         Next.js dashboard (App Router, shadcn/ui, TanStack Query).
infra/                       Terraform modules: network, data, kafka, compute, observability.
deploy/                      docker-compose.yml, Grafana/Prometheus/Loki/Tempo/Alloy config.
loadtest/                    k6 scripts.
docs/                        Spec, architecture, deploy, conventions, ADRs, results. See §7.
```

**Dependency direction is one-way:** `http / messaging(inbound) → service → domain ←
repository / cache / messaging(producers) / webhook`.
A handler must never import `repository`. A repository must never import `http`.

## 5. Non-negotiable code conventions

- **Layering:** handlers and consumers parse+validate+map; services decide; repositories persist.
  Business rules never live in a handler, a consumer loop, or SQL.
- **Errors:** define sentinel errors in `domain`, map them to HTTP once, in
  `internal/http/errors.go`. Handlers never build an error body by hand.
- **Response envelope:** every error response is
  `{"error":{"code":"RESOURCE_NOT_FOUND","message":"…","details":[]},"request_id":"…"}`.
- **Logging:** structured `log/slog` JSON only, with `trace_id`. No `fmt.Println`. Never log PII,
  API keys, signing secrets, tokens, passwords, or payload bodies (NFR-S5).
- **Config:** read env **only** in `internal/config`. Everywhere else takes a typed struct.
- **SQL:** parameterised always. No string concatenation. One transaction per use case,
  owned by the service layer via a `Tx` helper. Lists use keyset (cursor) pagination.
- **Tenancy:** every dashboard query is scoped by `org_id` taken from the auth context, never
  from the request body (FR-A3).
- **Outbound HTTP:** only through `internal/webhook` (SSRF guard, 15 s timeout, no redirects,
  capped body read — NFR-S1, S3).
- **Naming:** packages are short lowercase nouns; no `utils`, `common`, `helpers`, `base`.
- **Interfaces are declared by the consumer**, not the implementer, and stay small.

## 6. Definition of Done

An agent may only report completion when all of the following are true:

- [ ] `make check` passes locally.
- [ ] New/changed behaviour is covered by tests; edge + error paths included.
- [ ] Errors wrapped with context; no ignored `err`.
- [ ] Structured logs added at boundaries; no sensitive data.
- [ ] Metrics/tracing added if a new external call, topic or consumer was introduced.
- [ ] Migration written (and reversible) if the schema changed.
- [ ] `.env.example` and `docs/` updated if config or contracts changed; spec status updated.
- [ ] Commit follows Conventional Commits; PR template filled in.
- [ ] No TODO left without a linked issue.

## 7. Where to look (load on demand)

Do **not** read all of these at once. Open only what the task needs.

| Task | Read |
| --- | --- |
| What must the system do (FR/NFR) | `docs/SPEC.md` |
| Processes, topics, tables, Redis keys, code layering | `docs/ARCHITECT.md` |
| Milestones, scope, what's next | `docs/ROADMAP.md` |
| Adding/changing an endpoint | `docs/API.md`, `docs/CONVENTIONS.md` |
| Schema, queries, transactions, partitions | `docs/DATABASE.md` |
| Kafka topics, outbox, retries, DLQ, replay | `docs/MESSAGING.md` |
| Rate limits, caches, refresh tokens | `docs/CACHING.md` |
| Logs, metrics, traces, dashboards | `docs/OBSERVABILITY.md` |
| Writing tests | `docs/TESTING.md` |
| Local setup, ports, troubleshooting, AWS, Terraform, CI/CD | `docs/DEPLOY.md` |
| Incident / demo-session problems | `docs/RUNBOOK.md` |
| Big technical decision | `docs/adr/` + `docs/adr/0000-template.md` |
| Why the project exists, design history | `docs/superpowers/specs/2026-11-18-webhook-platform-design.md` |

## 8. Hard "do not"

- Do not `git push`, force-push, rebase shared branches, or create releases.
- Do not run `make migrate-down` against anything but your local database.
- Do not run `terraform apply`/`destroy` or touch AWS; that is a manual workflow (ADR-0004).
- Do not modify `.github/workflows/`, `.husky/`, or `.pre-commit-config.yaml` unless the task
  is explicitly about CI tooling.
- Do not add a framework (ORM, DI container, new router) to solve a local problem.
- Do not add Redis to the delivery pipeline's durable path (ADR-0003); retries live in Kafka.
- Do not "fix" a failing test by changing the assertion. Find the cause.
- Do not reformat or refactor files unrelated to the current task.
- Do not add comments that restate the code. Comment the *why*.

## 9. Working style

- Plan first for anything touching more than 3 files; show the plan, then execute.
- Prefer the smallest diff that fully solves the problem.
- When you finish, output: what changed, why, how it was verified, what you did not do.
- If a rule in this file blocks a genuinely better solution, say so instead of silently
  breaking the rule.
- Docs and code comments are in English.
