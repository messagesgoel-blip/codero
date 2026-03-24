# Agent Spec v3 — §9.1 Agent Interaction Contract

Evidence document for certification matrix clause §9.1.

## Contract

An agent has exactly **four** interactions with the Codero control plane:

| # | Interaction | Direction | Implementation Surface |
|---|-------------|-----------|----------------------|
| 1 | **Read task** | Agent ← Codero | `AcceptTask()` via CLI `session attach` / gRPC `GetAssignment` |
| 2 | **Write code** | Agent → repo | Agent-local; no Codero API involved |
| 3 | **Submit** | Agent → Codero | `EmitAssignmentUpdate()` via CLI `session emit` / gRPC `Submit` |
| 4 | **Read feedback** | Agent ← Codero | `GetFeedback()` via CLI / gRPC `GetFeedback` |

## Boundaries

- The agent **does not** call merge, heartbeat, monitor, reconcile, or gate RPCs.
- Heartbeat is managed by the launcher (`codero session heartbeat`), not the agent (§9.4).
- Substatus ownership is split: system-owned terminal states (`terminal_waiting_next_task`,
  `terminal_lost`, `terminal_stuck_abandoned`) cannot be emitted by agents (§9.2).
- The agent cannot bypass RULE-001 (gate must pass before merge) or any other compliance rule.

## Verification

- Code audit: no additional agent-facing RPCs exist beyond the four above.
- `internal/daemon/grpc/sessions.go` — RegisterSession (launcher), Heartbeat (launcher), GetSession (dashboard)
- `internal/daemon/grpc/assignments.go` — GetAssignment (agent), Submit (agent)
- `internal/daemon/grpc/feedback.go` — GetFeedback (agent)
- `internal/daemon/grpc/tasks.go` — IngestTask (daemon-internal)
- `internal/daemon/grpc/gate.go` — PostFindings (gate runner)
- No merge or workflow-mutation RPC exists for agents.
