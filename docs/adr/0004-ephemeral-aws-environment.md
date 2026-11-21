# ADR-0004: Ephemeral AWS environment, no live demo

- Status: accepted · Date: 2026-11-18

## Context

The project must show production AWS infrastructure as code, but a portfolio cannot carry a monthly
cloud bill.

## Options (us-east-1 prices, Nov 2026)

| Option | Cost | Against |
|---|---|---|
| Full managed stack running 24/7 | > $150/month | Too expensive |
| One EC2 + RDS running 24/7 | ≈ $50–65/month | Still a recurring bill; less representative architecture |
| Full stack in Terraform, apply/destroy on demand | a few dollars per demo session | No live link |

## Decision

Apply/destroy on demand from GitHub Actions. Evidence (video, Grafana screenshots, k6 results) lives
in the README.

## Consequences

`destroy` must leave nothing behind; a Budgets alert is part of the stack.
