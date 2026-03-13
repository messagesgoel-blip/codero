# Architecture Baseline

## Intent

Codero coordinates code review work across branches and repositories with explicit
state transitions, durable persistence, and observable operations.

## Boundaries

- Durable state: canonical source of truth (initially local DB)
- Coordination layer: ephemeral queue/lease/heartbeat primitives
- Interface layer: CLI + API contracts
- Operator layer: status surfaces, alerts, runbooks

## Initial Runtime Model

- Single daemon process
- Single operator deployment
- Contract-first module intake from prior systems

## Constraint

No capability is imported without contract definition and parity tests.
