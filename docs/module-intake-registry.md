# Module Intake Registry

Historical registry for the archived module-intake plan in `docs/roadmaps/archive/codero-roadmap-v5.md`.
Current execution priority is the Agents baseline plus Task Layer v2 in `docs/roadmap.md`.

Hard guardrail: no bulk copy from ghwatcher. Only module-level intake with contract + parity tests.

| Intake ID | Source Module | Target Domain | Status | Contract Doc | Parity Tests | Rollback Plan | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| MI-000 | N/A (pilot) | N/A (template) | stubbed | docs/contracts/mi-000-pilot-intake-stub.md | tests/parity/mi-000-pilot/ | N/A (stub only) | Pilot stub to validate intake template. No runtime impact. |
| MI-001 | lease semantics | coordination/lease | implemented | docs/contracts/mi-001-lease-semantics.md | internal/scheduler/*_test.go | lease TTL expiry | Historical Priority A. Implemented in the current baseline. |
| MI-002 | webhook dedup | ingestion/webhook | implemented | pending | internal/webhook/*_test.go | durable dedup + replay | Historical Priority A. Implemented; formal contract doc still optional. |
| MI-003 | relay claim/ack/resolve | delivery/relay | superseded | pending | pending | pending | Superseded by Task Layer v2 polling/feedback work unless a concrete delivery gap remains. |
| MI-004 | session heartbeat lifecycle | session/liveness | implemented | pending | internal/state/*_test.go | durable expiry + reconciliation | Historical Priority A. Implemented in the Agents v3 baseline. |
| MI-005 | mathkit-v2 two-pass local review gate | workflow/review-gate | phase1-prep | docs/contracts/review-gate-v1.md | scripts/review/two-pass-review.sh | docs/review-workflow.md | LiteLLM first pass + CodeRabbit second pass; parity validation pending |
