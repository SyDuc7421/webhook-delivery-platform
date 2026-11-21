# CLAUDE.md

**Read [`AGENTS.md`](./AGENTS.md) first — it is the source of truth for all conventions,
commands, layering rules and the Definition of Done. This file only adds Claude-specific
workflow.** (If you prefer, replace this file with a symlink: `ln -s AGENTS.md CLAUDE.md`.)

## Session start

1. Read `AGENTS.md` §2 (golden rules) and §4 (repo map). Check `docs/ROADMAP.md` for the
   current milestone.
2. Read only the `docs/` file the task needs (`AGENTS.md` §7). Do not preload all docs.
3. `git status` + `git log --oneline -10` to see where the repo is.

## Tool policy

- Prefer `rg` / `grep` over reading whole files. Read a file fully only when editing it.
- Use `make` targets, never raw `go test ./...`-style invocations.
- Batch independent reads/searches in one turn.
- Long or exploratory work (multi-file refactors, dependency audits, log spelunking):
  delegate to a subagent so the main context stays clean.

## Subagents (`.claude/agents/`)

| Agent | Use for |
| --- | --- |
| `code-reviewer` | Self-review a diff before opening a PR |
| `test-writer` | Generate table-driven tests for a package |
| `debugger` | Reproduce → isolate → fix a failing test or 5xx |

## Slash commands (`.claude/commands/`)

| Command | Purpose |
| --- | --- |
| `/new-endpoint` | Scaffold handler → service → repo → tests → docs, in order |
| `/new-consumer` | Scaffold a Kafka consumer loop with idempotency, retry tiers + DLQ |
| `/migration` | Create and fill a golang-migrate pair safely |
| `/review` | Run the pre-PR checklist against the current diff |
| `/adr` | Draft an ADR from the current discussion |

## Plan mode

Enter plan mode for: schema changes, new external dependency, new Kafka topic / key /
partition change, auth changes, anything touching >3 files. Get approval before editing.

## Stop conditions

Stop and ask the human when:
- the task requires a secret, a cloud credential, AWS access, or `terraform apply/destroy`;
- two conventions in `AGENTS.md` conflict for this task;
- the fix requires changing a public API contract;
- `make check` still fails after two honest attempts.
