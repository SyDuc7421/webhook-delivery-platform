# Webhook Delivery Platform — Design overview

Date: 2026-11-18 · Status: draft for approval · Working name: `webhook-platform` (rename freely)

This file is the entry point. The design itself lives in the project docs:

| Document | Answers |
|---|---|
| [spec.md](../../spec.md) | What the system must do (FR/NFR with IDs and milestones) |
| [architect.md](../../architect.md) | How it works: processes, Kafka topics, Postgres design, security, observability |
| [deploy.md](../../deploy.md) | Local compose, AWS (Terraform, Free plan), CI/CD |
| [TESTING.md](../../TESTING.md) | Test levels and what each proves |
| [roadmap.md](../../roadmap.md) | Milestones, estimates, stretch backlog |
| [adr/](../../adr/) | Decisions and the options rejected |

## Why this project exists

Portfolio project to get hired as a **remote fullstack engineer (Go + React)**. It sits next to
`shorten-link` and must tell a different story: write-heavy, reliability-first event delivery on
Kafka + PostgreSQL, with observability and AWS infrastructure as code. It closes the gaps found in the
job-market review: PostgreSQL in depth, AWS, testing at every level, CI/CD + Terraform, system design,
plus Kafka and observability.

## Goals, non-goals, anti-goals

**Goals**
- Kafka and Postgres each do work only they do well (per-key ordering, consumer groups, replay;
  outbox, partitioning, JSONB).
- At-least-once delivery proven by a failure test.
- One trace from API request to outbound HTTP call.
- Terraform that creates and destroys the full AWS architecture.
- Measured numbers (k6) and ADRs for the resume and interviews.

**Non-goals:** live always-on demo, multi-region, billing, SSO, email, exactly-once delivery.

**Anti-goals** (failure even if it works)
- Kafka, Redis or Postgres features bolted on without a reason in the domain.
- Scope that never reaches a presentable state at ~10 h/week.
- A surprise AWS bill.

## Constraints

- 1–2 h/day (≈10 h/week).
- Go backend, Next.js frontend; Kafka and PostgreSQL mandatory.
- AWS Free plan, ephemeral environment only.
- Monorepo; docs and code comments in English.
- Reuse conventions from `shorten-link` (layering, testcontainers, golang-migrate, OpenAPI, CI);
  logging is `log/slog` and routing is chi instead (ADR-0009, 2026-11-21)
  and the FE template (Next.js App Router, shadcn/ui, Tailwind, TanStack Query, next-intl, Vitest,
  Playwright, Husky/commitlint).

## Decisions taken during design

Approaches compared are in the ADRs.

| Decision | Where |
|---|---|
| Webhook delivery platform (over saga/CQRS and analytics) | ADR-0001 |
| Transactional outbox with polling relay | ADR-0002 |
| Kafka retry tiers + DLQ (over Redis ZSET and Postgres poller); first attempts ordered, retries not | ADR-0003 |
| Ephemeral AWS via apply/destroy, no live demo | ADR-0004 |
| Free plan, self-managed Kafka on EC2 instead of MSK | ADR-0005 |
| Monorepo | ADR-0006 |
| Redis caches (API key, subscriptions) added in M5 after a k6 baseline | ADR-0007 |
| chi + `log/slog` instead of Gin + zap (2026-11-21) | ADR-0009 |

Smaller calls (review these): payloads not schema-validated in the MVP; open registration creates an
org; 256 KB payload cap; test receiver is the only allowlisted internal host; region `us-east-1`;
create the AWS account when M6 starts; CI path filters per folder; rate limits fail open when Redis
is down; cache TTL ≤ 60 s with delete-on-write.

Open item for M6: custom domain for HTTPS on the ALB, or demo over the ALB's HTTP DNS name.

## Left to the builder

~~Package layout inside `/backend`~~ (see `AGENTS.md` §4), binary-per-role vs role flag, Kafka client library (franz-go
suggested), Postgres driver (pgx), partition-retention mechanism, exact retry delays and timeouts,
Terraform module internals, Grafana dashboard layout, shadcn components.
