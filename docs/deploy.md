# Deploy — webhook-platform

How the system runs locally and on AWS, and how CI/CD gets it there.

---

## 1. Local

One `deploy/docker-compose.yml` runs everything:

- PostgreSQL, Kafka (KRaft, single broker), Redis
- Prometheus, Loki, Tempo, Grafana
- the app processes (`api`, `relay`, `dispatcher`, `delivery-worker`, `retry-worker`) and `web`

Most development happens here at no cost. The same compose file is what CI runs Playwright e2e against.

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
