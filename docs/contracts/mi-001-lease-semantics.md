# Contract: MI-001 Lease Semantics

Status: implemented

## Purpose

Define the lease-semantics intake contract before importing implementation details.
This formalizes v5 sequencing: contract + parity tests are prepared in Phase 1,
then integration starts in Phase 2.

## Authoritative State Machine Reference

- Source of truth: v4 Canonical State Machine table
- Scope carried forward unchanged:
  - 10 states
  - 20 transitions

## Lease Semantics Scope

This contract covers lease behavior tied to state transitions, including:

1. Lease acquire
- only allowed for dispatch candidate in queued state
- atomic acquire with TTL (`SET NX EX` equivalent semantics)

2. Lease expiry
- expired lease transitions reviewing work back to queued/retry path
- retry accounting is deterministic and auditable

3. Lease release
- successful review completion clears lease and advances state
- release after failure restores queue eligibility per transition rules

4. Crash recovery
- on daemon restart, active reviewing entries are reconciled against live lease keys
- missing lease key + reviewing state is treated as expired/recoverable

## Redis Coordination Contract (Lease Portion)

- Key namespace must be deterministic and versionable
- TTL is mandatory for every active lease key
- Reconstructability: canonical state remains in durable store; Redis lease keys are rebuildable

## Implementation

### Package: `internal/scheduler`

#### Data Types

```go
type Lease struct {
    Repo       string    // owner/repo slug
    Branch     string    // branch name
    HolderID   string    // unique identifier of the lease holder
    ExpiresAt  time.Time // when the lease expires
    AcquiredAt time.Time // when the lease was acquired
}
```

#### Operations

- `LeaseManager.Acquire(ctx, repo, branch, holderID) → (*Lease, error)`
  - Returns `ErrLeaseConflict` if held by another holder
  - Same holder: extends existing lease

- `LeaseManager.Release(ctx, repo, branch, holderID) → error`
  - Returns `ErrLeaseNotFound` if held by another holder
  - Idempotent for non-existent leases

- `LeaseManager.Extend(ctx, repo, branch, holderID, ttl) → (*Lease, error)`
  - Returns `ErrLeaseNotFound` if held by another holder
  - Returns `ErrLeaseExpired` if lease has expired

- `LeaseManager.Get(ctx, repo, branch) → (*Lease, error)`
  - Returns nil if no lease exists

#### Heartbeat

```go
type HeartbeatConfig struct {
    Interval  time.Duration // default: 10s
    LeaseTTL  time.Duration // default: 30s
    MaxMisses int           // default: 3
}
```

- `LeaseManager.StartHeartbeat(ctx, lease, config) → *Heartbeat`
- `Heartbeat.Stop()` - releases lease and stops goroutine
- `Heartbeat.Status() → HeartbeatStatus`

### Redis Key Format

```
codero:<repo>:lease:<branch>
```

- Key type: STRING
- Value: holderID
- TTL: set on acquisition/extension

### Files

- `internal/scheduler/lease.go` - LeaseManager implementation
- `internal/scheduler/heartbeat.go` - Heartbeat implementation
- `internal/scheduler/queue.go` - WFQ queue and virtual time
- `internal/scheduler/lease_test.go` - Lease tests
- `internal/scheduler/heartbeat_test.go` - Heartbeat tests
- `internal/scheduler/queue_test.go` - Queue tests

## Parity Test Requirements (pre-integration)

Parity tests must validate:

1. valid lease-driven transition paths are accepted
2. invalid lease-driven transitions are rejected
3. lease expiry produces deterministic retry/state behavior
4. restart recovery behavior matches contract

## Out of Scope

- full webhook flow
- full relay delivery behavior
- multi-tenant controls

Those are separate intake modules.
