# Module Intake Registry

Each imported module must pass the intake workflow in roadmap v5.

Hard guardrail: no bulk copy from ghwatcher. Only module-level intake with contract + parity tests.

| Intake ID | Source Module | Target Domain | Status | Contract Doc | Parity Tests | Rollback Plan | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| MI-001 | lease semantics | coordination/lease | phase1-prep | docs/contracts/mi-001-lease-semantics.md | tests/contract/mi_001_lease_semantics_doc_test.go | pending | Priority A. Contract + parity prep in Sprint 3; integration starts Sprint 4. |
| MI-002 | webhook dedup | ingestion/webhook | planned | pending | pending | pending | Priority A |
| MI-003 | relay claim/ack/resolve | delivery/relay | planned | pending | pending | pending | Priority A |
| MI-004 | session heartbeat lifecycle | session/liveness | planned | pending | pending | pending | Priority A |
| MI-005 | mathkit-v2 two-pass local review gate | workflow/review-gate | integrated-v1 | docs/contracts/review-gate-v1.md | pending | docs/review-workflow.md | LiteLLM first pass + CodeRabbit second pass |
