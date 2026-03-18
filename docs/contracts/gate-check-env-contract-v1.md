# Contract: Gate-Check Env Vars v1 (GC-ENV-001)

Status: frozen
Sprint: COD-049
Package: `internal/gatecheck`

## Purpose

Document the `CODERO_*` environment contract used by gate-check so CLI, engine,
TUI, and dashboard remain deterministic and schema-compatible.

## Precedence Rules

1. CLI flag overrides env (e.g., `--report-path`).
2. Env overrides default.
3. Default is used when unset or invalid (see per-variable rules).

## Boolean Parsing

Accepted values (case-insensitive):
- `1`, `true` → `true`
- `0`, `false` → `false`

Any other value (including empty string) resolves to `false`.

## Numeric Parsing

- Empty string or invalid numeric values fall back to the default.
- Negative values are invalid and fall back to the default.
- Non-negative values (including `0`) are accepted when allowed by the field.

## String Parsing

- Unset → default.
- Empty string → explicit empty (used as-is).

## Variables

| Variable | Default | Notes |
|---|---|---|
| `CODERO_GATES_PROFILE` | `portable` | `strict` \| `portable` \| `off` (`fast` aliases `portable`); empty/invalid → `portable`. |
| `CODERO_REPO_PATH` | _empty_ | Repository root. Empty means use current working directory. |
| `CODERO_MAX_INFRA_BYPASS_GATES` | `2` | Non-negative. `0` means no infra bypasses are allowed before the budget fails a check. |
| `CODERO_ALLOW_REQUIRED_SKIP` | `false` | Boolean parsing rules apply. |
| `CODERO_GATE_TIMEOUT` | `120` | Seconds. Non-negative; `0` disables timeout. |
| `CODERO_MAX_STAGED_FILE_BYTES` | `1048576` | Bytes. Non-negative; `0` disables this size check threshold. |
| `CODERO_ENABLE_FAST_TESTS` | `false` | Feature flag. |
| `CODERO_ENABLE_NPM_AUDIT` | `false` | Feature flag. |
| `CODERO_ENABLE_DEPENDENCY_DRIFT_REPORT` | `false` | Feature flag. |
| `CODERO_ENFORCE_FORBIDDEN_PATHS` | `false` | Enables forbidden-paths check. **Both this and `CODERO_FORBIDDEN_PATH_REGEX` must be set to activate the check.** |
| `CODERO_FORBIDDEN_PATH_REGEX` | _empty_ | Regex used by forbidden-paths check. **Must be non-empty when `CODERO_ENFORCE_FORBIDDEN_PATHS=1`; empty disables the check even if enforce is set.** |
| `CODERO_ENFORCE_LOCKFILE_SYNC` | `false` | Enables lockfile-sync check. |
| `CODERO_ENFORCE_EXECUTABLE_POLICY` | `false` | Enables exec-bit-policy check. |
| `CODERO_ENFORCE_JSON_DUPLICATE_KEYS` | `false` | Enables duplicate-key check. |
| `CODERO_ENFORCE_SPDX_FOR_NEW_FILES` | `false` | Enables SPDX check. |
| `CODERO_ENFORCE_LICENSE_ON_RELEASE` | `false` | Enables release license check. |
| `CODERO_REQUIRED_CHECKS` | _empty_ | Comma-separated check IDs. Empty/unset → engine defaults. |
| `CODERO_OPTIONAL_CHECKS` | _empty_ | Comma-separated check IDs. Empty/unset → engine defaults. |
| `CODERO_TOOL_SHELLCHECK` | `shellcheck` | Tool path override. Empty forces missing-tool behavior. |
| `CODERO_TOOL_SEMGREP` | `semgrep` | Tool path override. Empty forces missing-tool behavior. |
| `CODERO_TOOL_GITLEAKS` | `gitleaks` | Tool path override. Empty forces missing-tool behavior. |
| `CODERO_TOOL_RUFF` | `ruff` | Tool path override. Empty forces missing-tool behavior. |
| `CODERO_TOOL_YAMLLINT` | `yamllint` | Tool path override. Empty forces missing-tool behavior. |
| `CODERO_GATE_CHECK_REPORT_PATH` | `.codero/gate-check/last-report.json` | Report persistence path (env overrides default; empty treated as default). |

## Report Path Precedence

`--report-path` > `CODERO_GATE_CHECK_REPORT_PATH` > `.codero/gate-check/last-report.json`
