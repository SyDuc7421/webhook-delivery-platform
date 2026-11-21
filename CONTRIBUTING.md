# Contributing

## 1. Prerequisites

Go 1.27 · Docker + Compose v2 · Node 24 + pnpm 10 (hooks; later `/web`) · Python 3.11+ (pre-commit) ·
`make`. Later milestones: k6 (M5), Terraform (M6).

```bash
git clone git@github.com:SyDuc7421/webhook-delivery-platform.git && cd webhook-delivery-platform
make setup     # installs Go tools, pnpm hook deps, pre-commit, copies .env.example -> .env
make up        # starts postgres, kafka (+ topics), redis, grafana, loki, tempo, prometheus, alloy
make migrate-up
make run
```

Health check: `curl localhost:8080/healthz` → `{"status":"ok"}`.

## 2. Branching

`main` is protected and always green (deploys are manual, ADR-0004). Branch from `main`:

```
feat/<ticket>-<slug>    fix/<ticket>-<slug>    chore/<slug>    docs/<slug>
```

Rebase on `main` before opening a PR. Squash-merge only.

## 3. Commits — Conventional Commits (enforced by commitlint)

```
<type>(<scope>)!: <subject>

[body]

Refs: #<issue> · FR-/NFR- ids
```

Types: `feat` `fix` `refactor` `perf` `test` `docs` `build` `ci` `chore` `revert`.
Scopes: `http` `service` `repo` `domain` `cache` `kafka` `webhook` `config` `obs` `migration`
`web` `infra` `deploy` `loadtest` `deps` `ci` `docs` `agents` (source: `commitlint.config.js`).
Subject: imperative, lowercase, ≤72 chars, no trailing period. `!` or a `BREAKING CHANGE:`
footer for breaking changes.

## 4. Quality gates

| Stage | Tool | Runs |
| --- | --- | --- |
| pre-commit | pre-commit + lint-staged | gofumpt, goimports, whitespace, secret scan, yaml/json lint, sqlfluff |
| commit-msg | commitlint | Conventional Commits |
| pre-push | Husky | `make check` |
| CI | GitHub Actions | path-filtered per folder: lint, unit, integration (testcontainers), race, coverage, vuln scan, image build |

Never bypass with `--no-verify`. If a hook is wrong, fix the hook in a `ci:` commit.

## 5. Pull requests

- Keep them under ~400 changed lines. Split otherwise.
- Fill the PR template: what/why, how tested, risk, rollback.
- Green CI required; self-review with `/review` (solo project) before merging.
- Schema, auth, or public contract changes need an ADR link.

## 6. Adding a dependency

Open an issue first with: problem, alternatives considered (stdlib? existing dep?), license,
maintenance status, binary-size/latency impact. Then `go get` + `go mod tidy` in a `build(deps)`
commit of its own.

## 7. Code review checklist

Correctness · layering respected · errors wrapped · context propagated · no N+1 · indexes for new
query patterns · idempotent consumers · offsets committed after durable work · no secrets in
logs/traces · tests cover error paths · migration reversible · spec status and docs updated.

## 8. Working with AI agents

Agents follow `AGENTS.md`. If a review comment reveals a rule an agent broke because the rule
was missing or unclear, update `AGENTS.md` in the same PR. Human-reviewed and human-owned:
agent output is a draft until a person approves it.
