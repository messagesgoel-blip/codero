# ADR 0007: Queue Architecture — Redis ZSET + SQLite Durability

Status: accepted

## Context

Codero requires a dispatch queue for coordinating branch reviews. The queue must support:
- Weighted-fair-queue (WFQ) scheduling to prevent starvation
- Priority-based ordering with aging bonuses
- Named queue classes (hotfix, release, experimental)
- Concurrent dispatch with lease-based coordination
- Recovery from coordinator failures

Two options were evaluated:
1. **River** — Postgres-backed job queue with built-in priorities, retries, and observability
2. **Current Redis ZSET + SQLite** — Redis for ephemeral queue ordering, SQLite for durable state

## Decision

**Keep the existing Redis ZSET + SQLite architecture.** Do not adopt River at this time.

### Rationale

1. **Alignment with existing ADRs**
   - ADR 0002 establishes SQLite as the durable canonical store
   - ADR 0003 establishes Redis as ephemeral coordination layer
   - The current queue design follows these boundaries precisely

2. **No production River usage**
   - River appears only in `go.mod` and `internal/deps/deps.go`
   - No integration code exists; adopting it would be greenfield work
   - Migration cost with no immediate payoff

3. **WFQ already implemented**
   - `ComputeWFQPriority()` with virtual time tracking
   - `ComputeAgingPriority()` for starvation prevention
   - `ClassifyWeight()` for named priority classes
   - All tested and working

4. **Recovery is designed-in**
   - Queue state is reconstructable from `branch_states` table in SQLite
   - Redis outage degrades throughput but loses no canonical state
   - Restart-safe by design (per ADR 0003)

5. **Operational simplicity**
   - Single-machine deployment target
   - Adding PostgreSQL would increase infrastructure complexity
   - Redis + SQLite is sufficient for current scale

## Tradeoffs

| Aspect | Redis + SQLite | River (Postgres) |
|--------|---------------|------------------|
| Infrastructure | Redis + SQLite | PostgreSQL only |
| Durability | SQLite (canonical) | Postgres (embedded) |
| Recovery | Rebuild from SQLite | Built-in |
| WFQ Support | Custom, tested | Priority queues |
| Observability | Custom metrics | Admin UI |
| Retry semantics | Manual | Built-in |
| Migration effort | None | High |

## Consequences

### Positive
- No migration effort required
- Existing queue tests pass (415 lines)
- Architecture remains consistent with ADRs 0002 and 0003
- Operational complexity unchanged

### Negative
- Queue state lost on Redis failure (acceptable per ADR 0003)
- Custom WFQ code requires ongoing maintenance
- No built-in retry/backoff (handled in runner layer)
- Observability requires custom instrumentation

### Mitigations
- Runner layer handles retries with exponential backoff
- Queue state rebuildable from `branch_states.state = 'queued_cli'`
- Metrics available via existing observability hooks

## Rollback Path

If River becomes necessary in the future:
1. Implement `riverdriver` adapter for queue operations
2. Migrate `branch_states.queue_priority` to River job metadata
3. Replace `scheduler.Queue` interface with River client
4. Keep SQLite as canonical state (River for dispatch only)

This would require:
- PostgreSQL instance (new infra)
- River schema migration
- Queue interface abstraction layer

## Evidence

Benchmark results (100 concurrent tasks, measured on ARM64):

```
BenchmarkQueue_100Concurrent-4   	     266	   9029837 ns/op
BenchmarkQueue_WFQFairness-4     	      96	  25654261 ns/op
TestMIG036_Queue_100Concurrent_Throughput: 21281 ops/sec
```

Throughput test: 20,000 operations (100 iterations × 200 ops each) completed in 939ms.

WFQ Fairness verified: high-weight branches (hotfix) scheduled proportionally more frequently.

All 51 scheduler tests pass:
- `TestQueueEnqueueDequeue`
- `TestQueueAlreadyEnqueued`
- `TestQueuePeek`
- `TestQueueRemove`
- `TestQueueList`
- `TestQueueUpdatePriority`
- `TestComputeWFQPriority`
- `TestVirtualTime`
- `TestComputeAgingPriority`
- `TestClassifyWeight`
- `TestQueueLen`
- `TestMIG036_Queue_100Concurrent_Throughput`
- `TestMIG036_Queue_WFQFairness`
- `TestMIG036_Queue_IntegrationWithFSM`
- `TestMIG036_VirtualTime_Monotonic`
- +36 lease/heartbeat/expiry tests

## References

- `internal/scheduler/queue.go` — Queue implementation
- `internal/scheduler/queue_test.go` — Test coverage
- `internal/runner/runner.go` — Queue consumer integration
- `internal/state/fsm.go` — FSM validation for state transitions
- `internal/state/state.go` — Canonical state definitions
- ADR 0002: Durable Store Strategy
- ADR 0003: Redis Role Boundaries