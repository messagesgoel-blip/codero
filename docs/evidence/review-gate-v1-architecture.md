# Review Gate v1 — Architecture & Scope Evidence

**Status:** Normative evidence for Review Gate v1 certification.
**Applies to:** Certification matrix §6 (RG-1 through RG-12).

---

## RG-2: progress.env Overwrite Semantics (Delegated)

The `progress.env` heartbeat file is written by the external `gate-heartbeat`
binary (`/srv/storage/shared/agent-toolkit/bin/gate-heartbeat`), not by the
Codero Go codebase. Codero's contract is:

- **Read**: `internal/daemon/observability.go:469` reads progress.env.
- **Contract**: The heartbeat binary overwrites (not appends) progress.env at
  each heartbeat interval.
- **Evidence**: `internal/gate/heartbeat_test.go` validates parsing of the
  overwritten content (PENDING, PASS, FAIL states).

This delegation is correct: the gate-heartbeat binary runs alongside the gate
runner and is responsible for heartbeat lifecycle. Codero consumes the result.

## RG-3: Primary Path — In-Process Findings

The daemon receives gate findings via the gRPC `PostFindings` API
(`internal/daemon/grpc/gate.go:25`). This is an in-process call from the gate
runner to the daemon — no external file exchange or HTTP callback is required.

Evidence: `internal/daemon/grpc/server_test.go:TestPostFindings_SuccessAndDuplicate`.

## RG-5: Symlink Exclusion — By Git Design

The engine collects staged files via `git diff --cached --name-only --diff-filter=ACM`
(`internal/gatecheck/engine.go:78`). The `ACM` filter includes only Added,
Copied, and Modified files. Symlink changes appear as type changes (`T`) in
git's diff output and are excluded by this filter.

This is not an explicit symlink check but a correct-by-construction filter that
naturally excludes symlinks from the staged file set.

## RG-8: AI Quorum — Gate Config Integration

AI quorum configuration is defined in the gate config registry:
- `CODERO_AI_QUORUM` (`internal/gate/config.go:103`) — default "1"
- `CODERO_MIN_AI_GATES` (`internal/gate/config.go:108`) — default "1"

The quorum logic is implemented in `scripts/review/two-pass-review.sh:295-297`:
```
if [ "$PASSED_COUNT" -ge "$MIN_SUCCESSFUL_AI_GATES" ]; then break
```

The quorum enforcement follows Gate Config v1 §7 and is tested in
`internal/gate/gate_config_v1_certification_test.go:TestCert_GCv1_S7_AISettingsRegistry`.

## RG-12: Feedback Standard Format

Gate findings are stored in the feedback cache via `PostFindings`
(`internal/daemon/grpc/gate.go:93-118`) and integrated into the feedback
aggregation pipeline (`internal/feedback/aggregator.go`). The aggregator
produces `FEEDBACK.md` using the same markdown format for all feedback sources.

Evidence: `internal/feedback/writer_test.go:59-72` validates FEEDBACK.md output.
