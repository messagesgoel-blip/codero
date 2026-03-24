# Contract: MI-006 Repo-Context

Status: implemented on `feat/RC-V1-closeout`; merge pending strict DoD certification

## Purpose

Define the Repo-Context intake contract before treating `internal/context` as a
completed Codero slice. Repo-Context v1 is additive and advisory-only: it must
never become a control-plane dependency for branch lifecycle state, gate
ordering, merge readiness, or daemon/session/task transitions.

## Scope

MI-006 covers the repo-local context subsystem described by
`Additional/codero_repo_context_v1.docx`:

1. Repo-local SQLite store at `.codero/context/graph.db`
2. Go-first AST indexer with full rebuild and incremental refresh
3. CLI contract for:
   - `codero context index`
   - `codero context status`
   - `codero context find`
   - `codero context grep`
   - `codero context symbols`
   - `codero context deps`
   - `codero context rdeps`
   - `codero context impact`
4. Advisory-only impact analysis and stable JSON vocabularies

## Interface Contract

### Storage

- Package: `internal/context`
- Store entrypoint: `OpenStore(path string) (*Store, error)`
- Default DB path: `.codero/context/graph.db`
- Durable metadata must include:
  - `schema_version`
  - `repo.root`
  - `last_indexed_at`
  - `last_indexed_sha`
  - `language_scope`

### Stable vocabularies

- `index_state`: `ready | missing | rebuild_required`
- `risk_level`: `low | medium | high`
- `analysis_state`: `ok | empty_input`
- `symbol.kind`: `package | file | function | method | struct | interface | type | const | var`
- `edge.kind`: `imports | defines | calls | references | embeds`
- `error.code`: `repo_not_found | repo_resolution_failed | db_error | index_missing | rebuild_required | subject_not_found | usage_error`

### CLI contract

- Every `codero context` subcommand accepts `--json`
- In JSON mode, stdout contains exactly one JSON object
- Empty result sets are successful outcomes with exit code `0`
- `subject_not_found` exits `1`
- usage/config errors exit `2`
- `impact` with no staged or explicit files returns `analysis_state: "empty_input"` and exit code `0`

## Parity / Contract Tests

The minimum MI-006 parity skeleton lives at:

- `internal/context/store_test.go`
- `internal/context/queries_test.go`
- `cmd/codero/context_cmd_test.go`

These tests must cover:

1. Store creation and metadata persistence
2. Query behavior for missing index, empty results, and advisory degradation
3. CLI JSON envelopes, stable vocabularies, sorting, and exit semantics
4. Advisory-only boundary: no workflow-state mutation or control-plane imports

## Rollback Notes

- Remove `.codero/context/graph.db`
- Remove `codero context` command wiring
- Delete `internal/context`
- Rebuild without repo-context until a corrected intake lands

## References

- `Additional/codero_repo_context_v1.docx`
- `codero_execution_loop_v1.docx`
- `docs/module-intake-registry.md`
