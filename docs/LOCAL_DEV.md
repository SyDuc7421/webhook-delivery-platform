# Local development

## First run

```bash
make setup      # go tools, npm (husky/commitlint/lint-staged), pre-commit, .env
make up         # postgres, kafka (+ topics), redis, observability — deploy/docker-compose.yml
make migrate-up
make run                    # api with air live reload on :8080
APP=relay make run          # any other process in another terminal
make up-apps                # or: every process in containers (profile "apps")
```

## Ports

| Service | Port |
| --- | --- |
| API | 8080 (`/metrics` on 9100) |
| Postgres | 5432 |
| Redis | 6379 |
| Kafka (host listener) | 9092 — containers use `kafka:29092` |
| Grafana | 3000 (admin/admin) |
| Web (Next.js, later) | 3001 |
| Prometheus | 9090 |
| Loki | 3100 |
| Tempo | 3200 |
| Alloy UI / OTLP | 12345 / 4317, 4318 |

Conflicts: override in `.env` (`API_PORT`, `POSTGRES_PORT`, `KAFKA_PORT`, …) — never edit
`deploy/docker-compose.yml` for a personal port clash.

## Tooling installed by `make setup`

`golangci-lint` v2 · `gofumpt` · `goimports` · `air` · `golang-migrate` · `sqlc` · `mockery` ·
`govulncheck` · `gitleaks` · `pre-commit` · `husky` + `commitlint` + `lint-staged`.
k6 (for `/loadtest`) and Terraform (for `/infra`) are installed by hand when their milestone
starts.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `make up` hangs on healthcheck | `make logs svc=kafka` / `svc=postgres`; stale volume → `make clean && make up` |
| `UNKNOWN_TOPIC_OR_PARTITION` | topics missing (auto-create is off) → `make topics` |
| App on host cannot reach Kafka | use `KAFKA_BROKERS=localhost:9092`; inside compose it is `kafka:29092` |
| `migrate: Dirty database version N` | `make migrate-force version=N-1`, fix the SQL, re-run |
| Port already allocated | change it in `.env`, or `make down` |
| Hooks not firing | `make setup` again (`npx husky` + `pre-commit install --hook-type commit-msg`) |
| Lint passes locally, fails in CI | version drift — `golangci-lint --version` vs `.github/workflows/ci.yml` |
| Grafana shows no logs | check Alloy container logs; docker socket must be mounted |
| No traces in Tempo | `OTEL_EXPORTER_OTLP_ENDPOINT` must point at Alloy (`localhost:4317` from host) |
| Outbound delivery to `localhost` blocked | expected (SSRF guard); use the built-in test receiver |

## Debugging

```bash
make logs svc=api                    # follow one service
make psql
docker compose -f deploy/docker-compose.yml exec redis redis-cli
docker compose -f deploy/docker-compose.yml exec kafka \
  /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server kafka:29092 --describe --all-groups
cd backend && dlv debug ./cmd/api    # or attach from your IDE on :2345
```
