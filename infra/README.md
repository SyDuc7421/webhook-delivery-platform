# infra — Terraform (milestone M6)

Not scaffolded yet. Modules: `network`, `data`, `kafka`, `compute`, `observability`; remote state
in S3; region `us-east-1`. Ephemeral only: apply/destroy from GitHub Actions (ADR-0004, ADR-0005).
See `docs/DEPLOY.md` §2. Never run `terraform apply`/`destroy` from an agent session.
