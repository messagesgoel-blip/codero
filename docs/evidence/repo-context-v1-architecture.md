# Repo-Context v1 — Certification Evidence (DOC)

Covers KATA-01, KATA-02, KATA-10, F-09, and §4 JSON contract
per `codero_certification_matrix_v1.md` §11.

## F-07 — Explainable results

**Implementation surface:** `internal/context/queries.go` — `computeRisk()`

Impact results include `reasons[]` explaining the risk classification.
Each reason maps directly to the rule that triggered (file count, symbol count,
or dependent count threshold).

**Evidence:** `TestImpact_RiskLevels` (7 cases) + `TestCert_RCv1_S4_4_RiskHeuristic` (6 cases)

## F-09 — Contract preservation

The `internal/context` package has zero imports of workflow-control packages
(`internal/state`, `internal/daemon`, `internal/session`, `internal/scheduler`,
`internal/webhook`, `internal/delivery`, `internal/feedback`).

Context commands do not modify branch state, gate ordering, merge readiness,
or daemon/session/task transitions.

**Evidence:** `TestCert_RCv1_KATA_AdvisoryOnlyBoundary` (source-level import scan)

## §4 — JSON contract

All 8 CLI commands wrap output in a consistent `Envelope`:

```json
{
  "schema_version": "1",
  "command": "<command>",
  "repo_root": "<path>",
  "generated_at": "<RFC3339>"
}
```

Each command has a dedicated response type embedding `Envelope`.
Existing CLI tests verify JSON structure end-to-end.

**Evidence:**
- `TestContextIndexFindGrepAndSymbols_JSONContracts` (cmd/codero)
- `TestContextStatusCmd_JSONMissingIndex` (cmd/codero)
- `TestContextDepsAndRdeps_SubjectNotFoundJSONError` (cmd/codero)
- `TestContextImpactCmd_EmptyInputJSONExit0` (cmd/codero)

## §4.9 — Exit codes

- `0` = success (including empty results, `empty_input` state)
- `1` = runtime failure (repo not found, DB error, subject not found)
- `2` = usage error (missing args, invalid regex)
- `status` always exits `0` per spec

**Evidence:** `TestContextFindCmd_UsageErrorJSONExit2`, `TestContextImpactCmd_EmptyInputJSONExit0`

## KATA-01 — Branch state not owned

Context never reads or writes branch lifecycle state. The `internal/context`
package has no access to `internal/state` (the durable session/branch store).

**Evidence:** `TestCert_RCv1_KATA_AdvisoryOnlyBoundary` + `go list` verification

## KATA-02 — Not source of truth for delivery

No review, delivery, or merge decisions are derived from context data.
The impact command returns advisory risk levels only.

**Evidence:** Architecture — no delivery/feedback/webhook imports in `internal/context`

## KATA-10 — Sweeper independence

The scheduler/sweeper package (`internal/scheduler`) has no import of
`internal/context`. Sweeper behavior is completely independent of context state.

**Evidence:** `go list -f '{{join .Imports "\\n"}}' ./internal/scheduler/` shows
no `internal/context` dependency.
