# ADR-0002: Transactional outbox between Postgres and Kafka

- Status: accepted · Date: 2026-11-18

## Context

An accepted event must never be lost (FR-I4). Writing to Postgres and producing to Kafka in the
request is a dual write: either can fail after the other succeeded.

## Options

| Option | For | Against |
|---|---|---|
| Produce to Kafka in the request, then insert | Lowest latency | Lost or orphaned events on partial failure; ingest fails when Kafka is down |
| Transactional outbox + polling relay | Event and outbox row commit atomically; ingest works while Kafka is down | Extra table and process; delivery latency includes the poll interval |
| Outbox + CDC (Debezium) | No polling | Another heavy component to run |

## Decision

Transactional outbox with a polling relay using `FOR UPDATE SKIP LOCKED`.

## Consequences

Relays can scale out. The outbox backlog becomes a key metric and alert. Duplicates are possible if a
relay crashes after producing and before deleting rows, which at-least-once delivery already accepts.
