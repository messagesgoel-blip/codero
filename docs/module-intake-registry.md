# Module Intake Registry

Each imported module must pass the intake workflow in roadmap v5.

Hard guardrail: no bulk copy from ghwatcher. Only module-level intake with contract + parity tests.

Classification and source-selection rules live in:

- `AGENTS.md`
- `docs/borrowed-components.md`
- `docs/roadmap-intake-map.md`

| Intake ID | Source Module | Target Domain | Status | Contract Doc | Parity Tests | Rollback Plan | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| MI-000 | N/A (pilot) | N/A (template) | stubbed | docs/contracts/mi-000-pilot-intake-stub.md | tests/parity/mi-000-pilot/ | N/A (stub only) | Pilot stub to validate intake template. No runtime impact. |
| MI-001 | lease semantics | coordination/lease | implemented | docs/contracts/mi-001-lease-semantics.md | internal/scheduler/*_test.go | lease TTL expiry | Priority A. Implemented P1-S3: queue/lease/heartbeat. |
| MI-002 | webhook dedup | ingestion/webhook | planned | pending | pending | pending | Priority A |
| MI-003 | relay claim/ack/resolve | delivery/relay | planned | pending | pending | pending | Priority A |
| MI-004 | session heartbeat lifecycle | session/liveness | planned | pending | pending | pending | Priority A |
| MI-005 | mathkit-v2 two-pass local review gate | workflow/review-gate | phase1-prep | docs/contracts/review-gate-v1.md | scripts/review/two-pass-review.sh | docs/review-workflow.md | LiteLLM first pass + CodeRabbit second pass; parity validation pending |
| MI-006 | repo-context AST graph index | advisory/repo-context | implemented | docs/contracts/mi-006-repo-context.md | internal/context/store_test.go; internal/context/queries_test.go; cmd/codero/context_cmd_test.go | delete `.codero/context/graph.db` and rebuild with `codero context index --full` | Repo-Context v1 intake for `internal/context`: interface contract, parity skeleton, CLI contract tests, advisory-only boundary, and repo-local SQLite graph store. |
