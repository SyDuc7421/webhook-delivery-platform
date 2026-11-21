# ADR-0001: Build a webhook delivery platform

- Status: accepted · Date: 2026-11-18

## Context

This is a portfolio project for remote fullstack (Go + React) roles. It must use Kafka and PostgreSQL
for real reasons, add observability, and tell a different story from `shorten-link` (read-heavy,
cache-first, MySQL + RabbitMQ).

## Options

| Option | For | Against |
|---|---|---|
| Webhook delivery platform | Kafka ordering/replay and Postgres outbox/partitioning are load-bearing; common Go-company domain; finishable scope | Dashboard-style frontend |
| Order/checkout with saga + CQRS | Shows distributed transactions | Heavy business logic, scope creep |
| Real-time analytics | Natural Kafka use | Repeats `shorten-link`'s click analytics |

## Decision

Webhook delivery platform.

## Consequences

The frontend is a developer dashboard rather than a consumer app; its quality matters because it is
what reviewers click first.
