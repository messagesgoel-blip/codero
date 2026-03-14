# Contract: Review Finding Schema (RF-001)

Status: implemented
Sprint: 5
Package: `internal/normalizer`

## Purpose

Define the canonical schema for normalized review findings produced by any
review provider and stored/delivered by codero. This schema is stable,
deterministic, and provider-agnostic.

## Canonical Finding Schema

```go
type Finding struct {
    Severity  Severity  // "error" | "warning" | "info"
    Category  string    // e.g. "security", "style", "logic", "general"
    File      string    // relative file path; empty = file-level finding
    Line      int       // 1-based line number; 0 = file-level
    Message   string    // human-readable description (required)
    Source    string    // originating provider or tool name
    Timestamp time.Time // UTC, truncated to second
    RuleID    string    // optional; provider-specific rule identifier
}
```

## Normalization Rules

| Field      | Rule |
|---|---|
| `severity` | Case-insensitive; aliased: `critical`/`fatal` → `error`, `medium` → `warning`, `note`/`suggestion`/`low`/`hint` → `info`; unknown → `info` |
| `category` | Lowercased; empty → `"general"` |
| `file`     | Trimmed whitespace; empty allowed (file-level finding) |
| `line`     | Negative → clamped to 0; 0 = file-level |
| `message`  | Required; trimmed; empty or whitespace-only → `ErrMalformedFinding` |
| `source`   | Uses `RawFinding.Source` if set, else the provider name parameter, else `"unknown"` |
| `timestamp`| `RawFinding.Timestamp` if non-zero; else normalization time; truncated to second, in UTC |
| `rule_id`  | Trimmed; empty allowed |

## Invariants

1. **Deterministic**: `Normalize(raw, source, now)` always produces the same
   `Finding` for the same inputs.
2. **No data loss**: malformed findings are returned as errors, not silently
   dropped. `NormalizeAll` returns both valid findings and a list of errors.
3. **Provider-agnostic**: no provider-specific field names leak into the
   canonical schema.
4. **Append-only storage**: findings are inserted into `findings` table and
   never updated or deleted.

## Error Behavior

- `ErrMalformedFinding` is returned when `message` is empty after trim.
- Unknown severity values default to `info` without error (providers vary widely).
- `NormalizeAll` processes all inputs and returns findings + errors for
  partial batches; callers receive maximum output.

## Provider Raw Format

Providers populate `normalizer.RawFinding`:

```go
type RawFinding struct {
    Severity  string
    Category  string
    File      string
    Line      int
    Message   string
    Source    string    // optional; overrides provider name param
    RuleID    string
    Timestamp time.Time // optional; zero = use normalization time
}
```

## Persistence

Normalized findings are stored in the `findings` table (see migration
`000002_sprint5_delivery.up.sql`) with `run_id` as a foreign key linking
to the review run that produced them.

## Delivery

Findings are delivered via the append-only delivery stream as a
`finding_bundle` event (see `delivery-replay-contract.md`). The payload
contains the full `[]Finding` slice alongside `run_id` and `provider`.
