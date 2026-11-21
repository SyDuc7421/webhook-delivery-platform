# What this repo contains

Started from the shared Go backend template; adapted to webhook-platform on 2026-11-21.

| Path | Why it exists |
| --- | --- |
| `AGENTS.md` | **Single source of truth** for every AI agent and every human. Rules, commands, layering, Definition of Done. |
| `CLAUDE.md` | Thin pointer to `AGENTS.md` + Claude-specific workflow (subagents, plan mode, stop conditions). |
| `.github/copilot-instructions.md` | Same pointer for Copilot. |
| `CONTRIBUTING.md` | Human workflow: branches, commits, PRs, quality gates. |
| `README.md` | Project pitch, quick start, links. |
| `SECURITY.md` | Reporting + security baseline (SSRF, signing, secrets). |
| `STRUCTURE.md` | This file. |
| `docs/spec.md` | What the system must do: FR/NFR with IDs, status and milestone. |
| `docs/architect.md` (+ `architect.html`, visual version) | System design: processes, Kafka topics, Postgres, Redis, security, observability, code layering. |
| `docs/deploy.md` | Local setup (ports, tooling, troubleshooting), AWS (Terraform, Free plan), CI/CD. |
| `docs/roadmap.md` | Milestones M0–M7, estimates, stretch backlog. |
| `docs/superpowers/specs/` | Design overview: why the project exists, goals, constraints, decision index. |
| `docs/CONVENTIONS.md` | Go style: packages, errors, structs, concurrency, logging. |
| `docs/API.md` | Surfaces, URLs, status codes, envelope, cursor pagination, chi handler skeleton. |
| `docs/DATABASE.md` | Migrations, schema rules, outbox, partitions, keyset queries, transactions. |
| `docs/MESSAGING.md` | Kafka topics, keys, record shape, at-least-once consuming, retry tiers, DLQ, replay. |
| `docs/CACHING.md` | Redis keys, TTL ≤ 60 s, delete-on-write, fail-open rules. |
| `docs/OBSERVABILITY.md` | slog → Loki, pipeline metrics, traces across Kafka, health, alerts. |
| `docs/TESTING.md` | Test levels (unit → failure → e2e → k6), Go test rules, must-cover paths. |
| `docs/RUNBOOK.md` | Symptom → check → fix, per alert. |
| `docs/adr/` | Decision records: `0000-template.md`, ADR-0001…0009. |
| `docs/results/` | k6 and `EXPLAIN ANALYZE` results, traceable to a commit. |
| `.claude/` | Shared permissions, slash commands (`/new-endpoint` `/new-consumer` `/migration` `/review` `/adr`), subagents. |
| `Makefile` | The only interface agents and humans use for tooling; Go targets run in `backend/`. |
| `backend/` | Go module: `cmd/`, `internal/*/doc.go` (package intent), `migrations/`, `test/`, `Dockerfile` (per-process via `APP` arg), `.golangci.yml`, `sqlc.yaml`, `.mockery.yaml`, `.air.toml`. |
| `deploy/docker-compose.yml` | Postgres, Kafka (KRaft) + `kafka-init` topics, Redis, Loki, Tempo, Alloy, Prometheus, Grafana; app processes under profile `apps`. |
| `deploy/observability/` | Loki, Tempo, Alloy, Prometheus (+ alert rules), Grafana provisioning and dashboards. |
| `web/`, `infra/`, `loadtest/` | Placeholders for M4, M6, M5. |
| `.pre-commit-config.yaml` | Fast checks: whitespace, gitleaks, hadolint, sqlfluff, gofumpt. |
| `.husky/` + `commitlint.config.js` + `package.json` | commit-msg + pre-push gates. |
| `.github/workflows/ci.yml` | Path-filtered CI: commitlint, lint, unit + coverage gate, integration, govulncheck, gitleaks, per-process image build + Trivy. |
| `.github/pull_request_template.md` `CODEOWNERS` `dependabot.yml` | Review hygiene. |
| `scripts/check-migrations.sh` | Blocks edits to already-applied migrations. |

## The design principle

**One source of truth, many thin pointers.** Every agent vendor wants its own filename
(`CLAUDE.md`, `copilot-instructions.md`, `AGENTS.md`). Duplicating rules across
them guarantees drift. So: all rules live in `AGENTS.md`; every other agent file is 3 lines that
say "read `AGENTS.md`".

**`AGENTS.md` stays short; `docs/` goes deep.** `AGENTS.md` is loaded on every turn, so it holds
only what is always true. `docs/` files are loaded on demand via the routing table in §7 — that
keeps context small and answers precise.

**Rules must be checkable.** "Write clean code" is noise. "`ctx` is the first parameter",
"`make check` must pass", "every Redis key has a TTL" are verifiable by a linter, a test, or a
reviewer — so agents actually comply.

**Machine enforcement beats prose.** Anything expressible in `.golangci.yml`, a pre-commit hook,
or CI belongs there, not in a Markdown rule. Docs are for what tooling cannot check.

## Keeping it alive

Treat `AGENTS.md` as code: when a review comment catches something an agent got wrong because a
rule was missing, add the rule **in the same PR** (`docs(agents): …`). Review the whole file
quarterly and delete rules nobody enforces — a stale rule teaches agents that rules are optional.
