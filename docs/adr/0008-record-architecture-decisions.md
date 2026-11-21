# ADR-0008: Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-11-21

## Context

Decisions about schema, messaging topology, auth, and dependencies get made in chat and are
forgotten. New team members — and AI agents — then re-litigate or silently contradict them.

## Decision

Every decision that is expensive to reverse is recorded as a numbered Markdown ADR in
`docs/adr/`, using `0000-template.md`. ADRs are immutable once accepted; a change means a new ADR
that supersedes the old one.

Triggers: new dependency · schema or index strategy · new Kafka topic, key or partition count ·
auth or authz change ·
public API contract change · anything an agent asks "should I…?" twice.

## Consequences

ADR-0001 to ADR-0007 were written during the design phase (2026-11-18) and follow this rule
retroactively. Small overhead per decision; large saving in context. `AGENTS.md` points agents at `docs/adr/`
so a decision written once is respected forever.
