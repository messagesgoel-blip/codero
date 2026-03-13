# ADR 0002: Durable Store Strategy

Status: accepted

## Context

Codero needs recoverable, durable state for branch lifecycle and events.

## Decision

Use a durable database as canonical source of truth. In early phases, keep this local-first
with explicit migrations and recoverability tests.

## Consequences

- Restart-safe state and auditability
- Migration discipline required from day one
- Coordination cache can be rebuilt from durable state
