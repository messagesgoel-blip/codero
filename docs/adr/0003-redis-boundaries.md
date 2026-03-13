# ADR 0003: Redis Role Boundaries

Status: accepted

## Context

Queue ordering, lease TTL, and heartbeat primitives require fast atomic coordination.

## Decision

Use Redis only as ephemeral coordination for queue/lease/liveness controls.
Durable state never lives only in Redis.

## Consequences

- Redis outages degrade throughput but do not lose canonical state
- Recovery routines must reconstruct Redis state from durable storage
- Coordination keys and scripts must be centralized and versioned
