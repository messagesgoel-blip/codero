# ADR 0001: Language and Runtime

Status: accepted

## Context

Codero requires a long-running daemon, strong concurrency primitives, simple deployment,
and predictable performance.

## Decision

Use Go as the primary implementation language and ship static binaries.

## Consequences

- Fast startup and low operational overhead
- Good fit for daemon + worker loops
- Requires discipline for package boundaries and contracts
