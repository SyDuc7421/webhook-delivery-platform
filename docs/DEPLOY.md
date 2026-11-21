# Deploy — webhook-platform

How the system runs locally and on AWS, and how CI/CD gets it there.

---

## 1. Local

One `deploy/docker-compose.yml` runs everything:

- PostgreSQL, Kafka (KRaft, single broker) + `kafka-init` (creates topics), Redis
- Prometheus, Loki, Tempo, Grafana, Alloy
- the app processes (`api`, `relay`, `dispatcher`, `delivery-worker`, `retry-worker`) under the
  `apps` profile, and later `web`

Most development happens here at no cost. The same compose file is what CI runs Playwright e2e against.

### First run

```bash
make setup      # go tools, npm (husky/commitlint/lint-staged), pre-commit, .env
make up         # postgres, kafka (+ topics), redis, observability — deploy/docker-compose.yml
make migrate-up
make run                    # api with air live reload on :8080
APP=relay make run          # any other process in another terminal
make up-apps                # or: every process in containers (profile "apps")
```

Every process serves `/metrics`, `/healthz` and `/readyz` on port 9100 (api also on 8080).
`/readyz` returns 503 only if a critical dependency (Postgres, Kafka) is down; Redis down is
reported but stays 200 (NFR-D1).

### Ports

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

### Tooling installed by `make setup`

`golangci-lint` v2 · `gofumpt` · `goimports` · `air` · `golang-migrate` · `sqlc` · `mockery` ·
`govulncheck` · `gitleaks` · `pre-commit` · `husky` + `commitlint` + `lint-staged`.
k6 (for `/loadtest`) and Terraform (for `/infra`) are installed by hand when their milestone
starts.

### Troubleshooting

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

### Debugging

```bash
make logs svc=api                    # follow one service
make psql
docker compose -f deploy/docker-compose.yml exec redis redis-cli
docker compose -f deploy/docker-compose.yml exec kafka \
  /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server kafka:29092 --describe --all-groups
cd backend && dlv debug ./cmd/api    # or attach from your IDE on :2345
```

---

## 2. AWS (ephemeral)

There is **no always-on environment**. The full stack is created for a demo session and destroyed
afterwards. See [ADR-0004](adr/0004-ephemeral-aws-environment.md).

| Layer | AWS resources |
|---|---|
| Network | VPC across 2 AZs, public and private subnets, security groups |
| Compute | ECS Fargate services for every process and `web`; ALB in front of `api` and `web` |
| Data | RDS PostgreSQL; ElastiCache Redis |
| Kafka | **Self-managed Kafka (KRaft) on one EC2 instance** in a private subnet, with an EBS volume |
| Images | ECR |
| Observability | Prometheus, Loki, Tempo, Grafana as ECS services (same config as local) |
| Cost guard | AWS Budgets alert |

Region: `us-east-1`.

### Terraform

Modules: `network`, `data`, `kafka`, `compute`, `observability`. Remote state in S3.
The `kafka` module owns broker config (listeners), security groups and the EBS volume.

### Account: Free plan

All services above are available on the AWS Free plan **except Amazon MSK**, which needs the Paid
plan; that is why Kafka is self-managed ([ADR-0005](adr/0005-self-managed-kafka-on-ec2.md)).

The Free plan ends 6 months after sign-up or when credits run out, whichever comes first, and the
account then closes. **Create the AWS account when milestone M6 starts**, not earlier.

### Open item

HTTPS on the ALB needs a domain and an ACM certificate. If no domain is available, demo over the
ALB's HTTP DNS name. Decide at the start of M6.

---

## 3. CI/CD (GitHub Actions)

| Trigger | Jobs |
|---|---|
| Pull request | Go vet, golangci-lint, `go test -race` (testcontainers), govulncheck · web typecheck, lint, test, build · Playwright e2e on compose · `terraform fmt -check`, `validate`, `tflint` |
| Push to `main` | Build images and push to ECR, authenticated with **OIDC** (no stored AWS keys) |
| Manual `deploy` | `terraform apply`, then roll out the latest images |
| Manual `destroy` | `terraform destroy` |

---

## 4. Demo session runbook (outline)

1. Run `deploy`. Wait for all ECS services to be healthy.
2. Run the k6 scenario and the scripted demo flow; record the video; take Grafana screenshots.
3. Run `destroy`. Confirm in the console that nothing is left running.
4. Commit results to `docs/results/` and update the README.
