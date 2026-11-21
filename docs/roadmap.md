# Roadmap — webhook-platform

Available time: 1–2 h/day (≈10 h/week). Hours below are rough guesses; re-estimate after 2–3 weeks
using real velocity.

## MVP milestones

| # | Milestone | Requirements | Est. hours |
|---|---|---|---|
| M0 | Monorepo, compose, CI skeleton, migration tooling | — | 8 |
| M1 | Accounts/orgs, API keys, event types, consumers/endpoints, ingest + outbox + relay, Redis rate limit | FR-A, FR-K, FR-T, FR-E1–4, FR-I, NFR-D1 | 20 |
| M2 | Dispatcher, delivery worker, signing, SSRF guard, partitioned attempts, retry tiers + DLQ, replay API | FR-E5, FR-D, FR-R, NFR-R2–3, NFR-S1, S3–4 | 25 |
| M3 | OpenTelemetry across Kafka, metrics, logs, provisioned dashboards and alerts | NFR-O, NFR-S5 | 12 |
| M4 | Web MVP screens + test receiver | FR-W, FR-X, NFR-S2 | 30 |
| M5 | Failure test, Playwright in CI, k6 baseline → Redis caches → k6 again | NFR-R1, NFR-P1–3 | 20 |
| M6 | Terraform modules, OIDC + ECR, deploy/destroy workflows, one AWS run with evidence | NFR-C | 25 |
| M7 | README, ADR review, architecture diagram, demo video | — | 8 |
| | **Total** | | **≈148 h ≈ 15 weeks** |

**Resume-ready checkpoint:** after M0–M3 plus a minimal M4 (≈ week 9), list the project on the resume
as "in progress" and start applying; finish M5–M7 in parallel.

## Stretch

After M7, in rough order of value:

1. Live event log (SSE)
2. Endpoint health mini-charts (success rate, p95)
3. Bulk replay by time range with a preview count
4. Embeddable consumer portal with short-lived tokens scoped to one consumer
5. Per-endpoint circuit breaker and send rate limit (Redis token bucket)
6. RBAC beyond owner/member
