# webhook-platform

Webhook delivery platform — a small Svix/Hookdeck. An org calls one API when something happens;
the platform delivers it as a signed HTTP POST to every subscribed endpoint, retries failures,
keeps per-endpoint order, records every attempt and supports replay.

Go (chi, slog) · PostgreSQL · Kafka · Redis · Next.js · OpenTelemetry + Grafana/Loki/Tempo/Prometheus ·
Terraform on AWS (ephemeral) · Docker Compose.

> Status: design done, building milestone M0. See [docs/roadmap.md](docs/roadmap.md).

## Quick start

```bash
make setup && make up && make migrate-up && make run
```

| Service | URL |
| --- | --- |
| API | http://localhost:8080 |
| Health / Ready | `/healthz` · `/readyz` |
| Metrics | http://localhost:9100/metrics |
| Kafka | `localhost:9092` |
| Grafana | http://localhost:3000 (admin/admin) |
| Prometheus | http://localhost:9090 |
| Tempo | http://localhost:3200 |

## Repository

| Path | Contents |
| --- | --- |
| `backend/` | Go module: `cmd/*` (api, relay, dispatcher, delivery-worker, retry-worker), `internal/*`, migrations |
| `web/` | Next.js dashboard (M4) |
| `infra/` | Terraform (M6) |
| `deploy/` | `docker-compose.yml`, observability config |
| `loadtest/` | k6 (M5) |
| `docs/` | spec, architecture, conventions, ADRs, results |

## Documentation

- [`docs/spec.md`](docs/spec.md) — what the system must do (FR/NFR with IDs)
- [`docs/architect.md`](docs/architect.md) — processes, Kafka topics, Postgres, Redis, security
- [`docs/deploy.md`](docs/deploy.md) — local, AWS, CI/CD
- [`docs/roadmap.md`](docs/roadmap.md) — milestones
- [`docs/adr/`](docs/adr) — decisions and rejected options
- [`AGENTS.md`](AGENTS.md) — conventions, commands, rules (humans read this too)
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — workflow, commits, PRs
