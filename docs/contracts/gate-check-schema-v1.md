# Contract: Gate-Check Schema v1 (GC-001)

Status: frozen
Sprint: COD-049
Package: `internal/gatecheck`

## Purpose

Freeze the canonical JSON schema emitted by the gate-check engine and consumed by
CLI, dashboard, and TUI surfaces.

## Schema Version

- `schema_version` is **always** `"1"` for this contract.
- Bump only on breaking schema changes.

## Status Vocabulary

`status` values for each check (and `overall_status` in summary):
- `pass`
- `fail`
- `skip`
- `disabled`

Notes:
- `overall_status` is only `pass` or `fail`.
- `skip` or `disabled` **must** include a `reason_code`.

## Reason Codes

Stable enum set for `reason_code` (lowercase):
- `user_disabled`
- `missing_tool`
- `not_applicable`
- `not_in_scope`
- `timeout`
- `infra_bypass`
- `infra_auth`
- `infra_rate_limit`
- `infra_network`
- `exec_error`

## Canonical JSON Shape

```json
{
  "summary": {
    "overall_status": "pass",
    "passed": 4,
    "failed": 0,
    "skipped": 5,
    "infra_bypassed": 0,
    "disabled": 4,
    "total": 13,
    "required_failed": 0,
    "required_disabled": 0,
    "profile": "portable",
    "schema_version": "1"
  },
  "checks": [
    {
      "id": "file-size",
      "name": "File size limit",
      "group": "format",
      "required": true,
      "enabled": true,
      "status": "skip",
      "reason_code": "not_in_scope",
      "reason": "no staged files",
      "tool_name": "",
      "tool_path": "",
      "tool_version": "",
      "duration_ms": 0,
      "details": ""
    }
  ],
  "run_at": "2025-03-17T10:00:00Z"
}
```

## Determinism Rules

1. **Order-stable checks**: `checks` are emitted in the fixed runner order:
   - `file-size`
   - `merge-markers`
   - `trailing-whitespace`
   - `final-newline`
   - `forbidden-paths`
   - `config-validation`
   - `lockfile-sync`
   - `exec-bit-policy`
   - `gofmt`
   - `gitleaks-staged`
   - `semgrep`
   - `ruff-lint`
   - `ai-gate`
2. **Deterministic summary**: aggregation is purely derived from the emitted checks.
3. **All checks present**: no check is omitted; disabled/skipped checks are still listed.

## Field Semantics

| Field | Notes |
|---|---|
| `summary.infra_bypassed` | Count of checks with infra-classified reason codes (`timeout`, `infra_*`, `infra_bypass`). |
| `checks[].enabled` | True if the check was eligible to run (tool/config present). |
| `checks[].reason_code` | Required for `skip`/`disabled`. Optional otherwise. |
| `checks[].reason` | Human-readable reason (optional). |
| `checks[].duration_ms` | Milliseconds spent in the check runner. |

## Compatibility Guarantee

This schema is the single source of truth for CLI JSON, dashboard `/gate-checks`,
and TUI adapters. Any future change requires a schema version bump and updated
contract tests.
