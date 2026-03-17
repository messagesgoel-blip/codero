# Gate Check Engine — Runbook (COD-048)

The **gate-check engine** (v2) is a deterministic local pre-commit gate runner
that reports every check's status using the canonical check model. Unlike the
AI-driven `commit-gate`, this engine runs entirely locally and classifies every
check as `PASS`, `FAIL`, `SKIP`, `INFRA_BYPASS`, or `DISABLED` — never silently
dropping a check from output.

---

## Quick start

```bash
# Run with default portable profile (missing tools → DISABLED, not FAIL)
codero gate-check

# Emit canonical JSON to stdout
codero gate-check --json

# Write JSON report for dashboard consumption
codero gate-check --json --report-path .codero/gate-check/last-report.json

# Strict profile: missing required tools fail the gate
codero gate-check --profile strict

# Profile=off: skip almost everything (useful for pipelines without tools)
codero gate-check --profile off
```

---

## Profiles

| Profile | Missing required tool | Missing optional tool | Infra error |
|---|---|---|---|
| `strict` | `DISABLED` → overall **FAIL** | `DISABLED` | classified per type |
| `portable` | `DISABLED` (no overall fail) | `DISABLED` | classified per type |
| `off` | All checks → `SKIP`/`DISABLED` | All checks → `SKIP`/`DISABLED` | N/A |

Set via `--profile` flag or `CODERO_GATES_PROFILE` env var.

---

## Check inventory

| ID | Group | Required | Notes |
|---|---|---|---|
| `file-size` | format | ✓ | Fails if any staged file exceeds `CODERO_MAX_STAGED_FILE_BYTES` (default 1 MiB) |
| `merge-markers` | format | ✓ | Detects `<<<<<<<`/`=======`/`>>>>>>>` in staged files |
| `trailing-whitespace` | format | – | Trailing spaces/tabs on any line |
| `final-newline` | format | – | File must end with `\n` |
| `forbidden-paths` | config | ✓ | Requires `CODERO_ENFORCE_FORBIDDEN_PATHS=1` + `CODERO_FORBIDDEN_PATH_REGEX` |
| `config-validation` | config | ✓ | Validates staged `.json` and `.yaml`/`.yml` files |
| `lockfile-sync` | config | – | Requires `CODERO_ENFORCE_LOCKFILE_SYNC=1`; checks `go.mod`↔`go.sum` and `package.json`↔`package-lock.json` |
| `exec-bit-policy` | config | – | Requires `CODERO_ENFORCE_EXECUTABLE_POLICY=1`; non-`.sh` files must not have `+x` |
| `gofmt` | format | – | Runs `gofmt -l` on staged `.go` files; `DISABLED` if gofmt missing |
| `gitleaks-staged` | security | ✓ | Runs `gitleaks protect --staged`; `DISABLED` if gitleaks missing |
| `semgrep` | security | – | Runs `semgrep --config auto`; `DISABLED` if semgrep missing |
| `ruff-lint` | lint | – | Runs `ruff check` on staged `.py` files; `DISABLED` if ruff missing |
| `ai-gate` | ai | – | Always `DISABLED` (`NOT_IN_SCOPE`); AI review runs via `codero commit-gate` |

---

## Canonical status model

```json
{
  "summary": {
    "overall_status": "PASS",
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
      "status": "SKIP",
      "reason_code": "NOT_IN_SCOPE",
      "reason": "no staged files",
      "duration_ms": 0
    },
    {
      "id": "gitleaks-staged",
      "name": "Secret scan (gitleaks)",
      "group": "security",
      "required": true,
      "enabled": false,
      "status": "DISABLED",
      "reason_code": "MISSING_TOOL",
      "reason": "gitleaks not found",
      "tool_name": "gitleaks",
      "duration_ms": 1
    },
    {
      "id": "ai-gate",
      "name": "AI review gate",
      "group": "ai",
      "required": false,
      "enabled": false,
      "status": "DISABLED",
      "reason_code": "NOT_IN_SCOPE",
      "reason": "AI gate is run separately via `codero commit-gate`",
      "duration_ms": 0
    }
  ],
  "run_at": "2025-03-17T10:00:00Z"
}
```

### Status values

| Status | Meaning |
|---|---|
| `PASS` | Check ran and passed |
| `FAIL` | Check ran and detected a problem |
| `SKIP` | Check enabled but not applicable (e.g. no staged files of that type) |
| `INFRA_BYPASS` | Tool ran but infrastructure prevented a definitive result |
| `DISABLED` | Check not run; see `reason_code` for why |

### Reason codes (non-pass)

| Code | Meaning |
|---|---|
| `USER_DISABLED` | Disabled by env flag (e.g. `CODERO_ENFORCE_LOCKFILE_SYNC=0`) |
| `MISSING_TOOL` | Required binary not found in PATH |
| `NOT_APPLICABLE` | Check not applicable to this repo/context |
| `NOT_IN_SCOPE` | Check is out of scope for this runner (e.g. AI gate) |
| `INFRA_TIMEOUT` | Tool timed out |
| `INFRA_AUTH` | Tool returned auth/401 error |
| `INFRA_RATE_LIMIT` | Tool was rate-limited (429) |
| `INFRA_NETWORK` | Tool encountered a network error |
| `EXEC_ERROR` | Tool ran but exited with an unexpected error |

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `CODERO_GATES_PROFILE` | `portable` | Profile: `strict` \| `portable` \| `off` |
| `CODERO_REQUIRED_CHECKS` | _(engine default)_ | Comma-separated check IDs to force as required |
| `CODERO_OPTIONAL_CHECKS` | _(engine default)_ | Comma-separated check IDs to force as optional |
| `CODERO_ALLOW_REQUIRED_SKIP` | `0` | Allow required checks to be `DISABLED` without causing FAIL |
| `CODERO_MAX_INFRA_BYPASS_GATES` | `2` | Max checks that may be `INFRA_BYPASS` before budget fails required checks |
| `CODERO_GATE_TIMEOUT` | `120` | Per-engine timeout in seconds |
| `CODERO_MAX_STAGED_FILE_BYTES` | `1048576` | Max bytes per staged file (file-size check) |
| `CODERO_ENABLE_FAST_TESTS` | `0` | Enable related test runner (not yet wired) |
| `CODERO_ENABLE_NPM_AUDIT` | `0` | Enable npm audit check (not yet wired) |
| `CODERO_ENFORCE_FORBIDDEN_PATHS` | `0` | Enable forbidden-paths check |
| `CODERO_FORBIDDEN_PATH_REGEX` | _(none)_ | Regex of path patterns to block |
| `CODERO_ENFORCE_LOCKFILE_SYNC` | `0` | Enable lockfile-sync check |
| `CODERO_ENFORCE_EXECUTABLE_POLICY` | `0` | Enable exec-bit-policy check |
| `CODERO_TOOL_SHELLCHECK` | `shellcheck` | Path override for shellcheck |
| `CODERO_TOOL_SEMGREP` | `semgrep` | Path override for semgrep |
| `CODERO_TOOL_GITLEAKS` | `gitleaks` | Path override for gitleaks |
| `CODERO_TOOL_RUFF` | `ruff` | Path override for ruff |
| `CODERO_TOOL_YAMLLINT` | `yamllint` | Path override for yamllint |
| `CODERO_GATE_CHECK_REPORT_PATH` | _(none)_ | Path where the CLI writes the JSON report (read by dashboard) |

---

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Overall `PASS` |
| `1` | Overall `FAIL` or runtime error |

---

## Missing tool behavior example

```
$ codero gate-check
ID                      GROUP       STATUS        REQ   TOOL        REASON
──────────────────────  ──────────  ────────────  ───   ──────────  ──────────────────────────────────
file-size               format      SKIP          req               no staged files
merge-markers           format      SKIP          req               no staged files
trailing-whitespace     format      SKIP          opt               no staged files
final-newline           format      SKIP          opt               no staged files
forbidden-paths         config      DISABLED      req               CODERO_ENFORCE_FORBIDDEN_PATHS not set
config-validation       config      SKIP          req               no JSON/YAML staged files
lockfile-sync           config      DISABLED      opt               CODERO_ENFORCE_LOCKFILE_SYNC not set
exec-bit-policy         config      DISABLED      opt               CODERO_ENFORCE_EXECUTABLE_POLICY not set
gofmt                   format      SKIP          opt               no staged .go files
gitleaks-staged         security    DISABLED      req   gitleaks    MISSING_TOOL
semgrep                 security    SKIP          opt               no staged files
ruff-lint               lint        SKIP          opt               no staged .py files
ai-gate                 ai          DISABLED      opt               NOT_IN_SCOPE

Summary  pass=0  fail=0  skip=8  infra=0  disabled=5  total=13  profile=portable
gate-check: ✅ PASS
```

In `portable` profile, `gitleaks-staged` being `DISABLED` (missing tool) does **not**
cause an overall `FAIL`. Switch to `--profile strict` to enforce that all required
tools must be present.

---

## Dashboard integration

The dashboard endpoint `GET /api/v1/dashboard/gate-checks` reads the last report
written by the CLI. To wire them together:

```bash
# Write report after a gate-check run
codero gate-check --report-path .codero/gate-check/last-report.json

# Or configure via env
export CODERO_GATE_CHECK_REPORT_PATH=.codero/gate-check/last-report.json
codero gate-check
```

The dashboard serves the raw canonical JSON wrapped in:
```json
{
  "report": { ... },
  "generated_at": "2025-03-17T10:05:00Z"
}
```

When no report has been written yet, the endpoint returns:
```json
{
  "report": null,
  "message": "no gate-check report available; run `codero gate-check` to generate one"
}
```

---

## TUI integration

The `ChecksPane` component in the TUI displays all checks from a gate-check
report. Checks are shown with their status icon, group, and reason:

```
  GATE CHECKS
  ──────────────────────────────────────────────────────────
  pass=4  fail=0  skip=7  infra=0  disabled=2  [portable]

  ✓ file-size               format      PASS    req
  ○ merge-markers           format      SKIP    req    no staged files
  – gitleaks-staged         security    DISABLED req   MISSING_TOOL
  – ai-gate                 ai          DISABLED opt   NOT_IN_SCOPE
```

Status icon legend:
- `✓` PASS
- `✗` FAIL
- `○` SKIP
- `⚡` INFRA_BYPASS
- `–` DISABLED

Rows with `DISABLED` and `SKIP` status are always rendered; they are never hidden.
