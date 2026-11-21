# ADR-0006: Monorepo

- Status: accepted · Date: 2026-11-18

## Context

`shorten-link` uses separate backend and frontend repositories. Here, e2e tests, docker-compose and
Terraform all need the backend and the frontend together.

## Decision

One repository with `/backend`, `/web`, `/infra`, `/deploy`, `/loadtest` and `/docs`.

## Consequences

CI uses path filters so a web-only change does not run the Go jobs, and the reverse.
