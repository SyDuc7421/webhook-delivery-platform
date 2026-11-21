---
description: Scaffold a new HTTP endpoint through every layer, in order
argument-hint: <METHOD> <path> — e.g. POST /v1/consumers/{consumer_id}/endpoints
---

Read `docs/API.md`, `docs/CONVENTIONS.md` and the FR in `docs/spec.md` first. Then implement `$ARGUMENTS` in this order,
stopping to show me the plan before you write code:

1. **Contract** — request/response DTOs in `internal/http/dto/`, with `validate` tags. Define the
   error codes this endpoint can return.
2. **Domain** — any new entity/value object + sentinel errors in `internal/domain/`.
3. **Repository** — interface in `domain`, implementation in `internal/repository/postgres/`.
   Add a migration via `make migrate-new` if the schema changes. Add the index the new query
   needs.
4. **Service** — use case in `internal/service/`. Transactions, authorization, business rules
   live here; every query is scoped by `org_id`. Invalidate any Redis cache entry the write affects.
5. **Handler** — thin: bind → validate → map → call service → map error → respond. Use the
   central `respondError` helper. Never build an error body inline.
6. **Route** — register in `internal/http/router.go` (chi) in the right group (API key / JWT / public).
7. **Tests** — table-driven unit tests for the service (happy, validation, not-found, conflict,
   dependency failure) + an integration test for the route.
8. **Docs** — update `docs/openapi.yaml`, `docs/API.md` if a convention was extended, and the status of the FR in `docs/spec.md`.

Finish with `make check` and report: files added/changed, error codes introduced, migration name,
what is not covered.
