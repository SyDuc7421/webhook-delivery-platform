# Testing — webhook-platform

What is tested at each level, what each level proves, and the rules every Go test follows.

## Levels

| Level | Tooling | Covers |
| --- | --- | --- |
| Go unit | `go test` | signing, retry schedule, SSRF guard, idempotency rules, pagination cursors |
| Go integration | testcontainers (Postgres, Kafka, Redis) via `backend/test/testenv`, `//go:build integration` | migrations up/down, adapters, repositories, outbox relay, dispatcher fan-out, worker offset handling |
| Failure | testcontainers + process kill | NFR-R1: kill the delivery worker mid-batch; every event still reaches the receiver at least once |
| Degraded mode | testcontainers, stop Redis mid-test | NFR-D1: ingest and delivery continue without Redis |
| Cache correctness | Go integration | NFR-P3: a revoked key and a disabled endpoint take effect on the next request |
| Web unit | Vitest | components and view-models |
| End-to-end | Playwright against `deploy/docker-compose.yml` | create endpoint → send test event → receiver returns 500 → replay → succeeds |
| Load | k6 (`/loadtest`) | throughput, end-to-end p95, consumer-lag recovery (NFR-P1); same scenario before and after Redis caches (NFR-P2) |

Coverage target: ≥80% on `service/` and `domain/`, ≥60% overall (CI gate). Coverage is a smell
detector, not a goal — a covered line with no assertion is worthless.

Kafka in integration tests is `confluentinc/confluent-local:8.4.1` (Kafka 4.x) (the testcontainers Kafka module only
drives Confluent images); compose and AWS run `apache/kafka`. Both speak the same client protocol.

## Go rules

- **Table-driven** by default:

```go
tests := []struct {
    name    string
    input   service.IngestInput
    setup   func(*mocks.MockEventRepository)
    want    domain.Event
    wantErr error
}{...}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()
        ...
    })
}
```

- `t.Parallel()` everywhere it is safe. `-race` is on in CI.
- Mock **interfaces you own** (repos, producers, clocks, HTTP sender). Never mock Postgres, Kafka
  or Redis — use a real one in integration tests.
- Time and randomness are injected (`Clock`, `IDGen`). No `time.Now()` inside a service. Retry-tier
  delays are tested with a fake clock, never with `time.Sleep`.
- Outbound HTTP is tested against `httptest.Server`. SSRF-guard tests must include DNS names that
  resolve to private, loopback, link-local, CGNAT and metadata addresses.
- Each test creates its own data (fixtures/builders) and cleans up with `t.Cleanup`. No shared
  mutable global state, no test ordering dependencies. Kafka tests use unique topic names per test.
- Assert on behaviour and error identity (`errors.Is`), not on log output or exact wording.
- Every bug fix starts with a failing test that reproduces it.

## Must-cover paths

Validation rejects bad input (bad JSON, > 256 KB) · org scoping denies cross-org access ·
not-found → 404 · conflict → 409 · repeated `Idempotency-Key` returns the original event ·
ingest succeeds while Kafka is down (event waits in outbox) · revoked API key rejected ·
disabled endpoint receives nothing · non-2xx/timeout → next retry tier · last tier → `dead` + DLQ ·
replay keeps the same `webhook-id` · consumer handles duplicate records (no duplicate `messages`
rows) · offset committed only after the attempt is recorded · Redis down → rate limit fails open ·
graceful shutdown finishes or hands back in-flight work.

## CI gates

Every pull request runs Go unit + integration tests with `-race`, web unit tests and Playwright e2e.
See [DEPLOY.md](DEPLOY.md#3-cicd-github-actions).

## Load test results

k6 results are committed to `docs/results/` with the date, environment (local or AWS) and commit
hash, so every number in the README can be traced to a run.

## Commands

```bash
make test                  # unit, -race -shuffle=on
make test-int              # integration (testcontainers; needs Docker)
make cover                 # HTML coverage report
cd backend && go test -run TestX ./internal/service -v
```
