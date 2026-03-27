# Gate-Check Activation Guide

This guide explains how to activate optional gate checks in Codero. Optional checks are disabled by default and require environment variables to enable.

---

## Quick Reference

| Check | Env Variable | Required | Blocks Commit |
|-------|--------------|----------|---------------|
| forbidden-paths | `CODERO_ENFORCE_FORBIDDEN_PATHS=1` + `CODERO_FORBIDDEN_PATH_REGEX` | Yes | Yes |
| lockfile-sync | `CODERO_ENFORCE_LOCKFILE_SYNC=1` | No | Yes (on mismatch) |
| exec-bit-policy | `CODERO_ENFORCE_EXECUTABLE_POLICY=1` | No | Yes (on violation) |

---

## forbidden-paths

**Purpose:** Block commits that contain files matching a forbidden path pattern.

### Activation

Requires **both** environment variables:

```bash
export CODERO_ENFORCE_FORBIDDEN_PATHS=1
export CODERO_FORBIDDEN_PATH_REGEX="(secrets/|\\.env$|\\.pem$)"
```

| Variable | Required | Description |
|----------|----------|-------------|
| `CODERO_ENFORCE_FORBIDDEN_PATHS` | Yes | Set to `1` or `true` to enable |
| `CODERO_FORBIDDEN_PATH_REGEX` | Yes | Go regex pattern; must be non-empty |

### Behavior When Enabled

- **Pass:** No staged files match the regex pattern
- **Fail:** One or more staged files match; commit blocked with details
- **Fail (invalid regex):** Check fails with `invalid CODERO_FORBIDDEN_PATH_REGEX: <error>`

### Example Output

**Pass:**
```
forbidden-paths    config    pass    req
```

**Fail:**
```
forbidden-paths    config    fail    req    forbidden paths: secrets/api-key.pem, .env
```

**Disabled (default):**
```
forbidden-paths    config    disabled    req    CODERO_ENFORCE_FORBIDDEN_PATHS not set
```

**Misconfigured (regex missing):**
```
forbidden-paths    config    disabled    req    CODERO_FORBIDDEN_PATH_REGEX not set or empty
```

### Common Patterns

```bash
# Block secrets directories and .env files
CODERO_FORBIDDEN_PATH_REGEX="(secrets/|\\.env$)"

# Block certificate and key files
CODERO_FORBIDDEN_PATH_REGEX="\\.(pem|key|crt)$"

# Block multiple patterns
CODERO_FORBIDDEN_PATH_REGEX="(credentials/|private/|\\.pem$|\\.key$)"
```

---

## lockfile-sync

**Purpose:** Ensure lockfiles are committed alongside their manifest files.

### Activation

```bash
export CODERO_ENFORCE_LOCKFILE_SYNC=1
```

| Variable | Required | Description |
|----------|----------|-------------|
| `CODERO_ENFORCE_LOCKFILE_SYNC` | Yes | Set to `1` or `true` to enable |

### Behavior When Enabled

Checks for these pairs:
- `go.mod` ↔ `go.sum`
- `package.json` ↔ `package-lock.json`

- **Pass:** Both files in each pair are staged together
- **Fail:** Manifest staged without corresponding lockfile
- **Skip:** No lockfile-relevant files staged

### Example Output

**Pass:**
```
lockfile-sync    config    pass    opt
```

**Fail:**
```
lockfile-sync    config    fail    opt    go.mod staged without go.sum
```

**Skip (no relevant files):**
```
lockfile-sync    config    skip    opt    no lockfile pair staged
```

**Disabled (default):**
```
lockfile-sync    config    disabled    opt    CODERO_ENFORCE_LOCKFILE_SYNC not set
```

---

## exec-bit-policy

**Purpose:** Detect unexpected executable bits on non-shell files.

### Activation

```bash
export CODERO_ENFORCE_EXECUTABLE_POLICY=1
```

| Variable | Required | Description |
|----------|----------|-------------|
| `CODERO_ENFORCE_EXECUTABLE_POLICY` | Yes | Set to `1` or `true` to enable |

### Behavior When Enabled

- **Pass:** No non-`.sh` files have the executable bit set
- **Fail:** Non-shell files have `+x`; commit blocked with file list
- **Skip:** No staged files

Files with `.sh` extension are allowed to have the executable bit.

### Example Output

**Pass:**
```
exec-bit-policy    config    pass    opt
```

**Fail:**
```
exec-bit-policy    config    fail    opt    unexpected executable bit on non-shell files: main.go, utils.py
```

**Skip (no staged files):**
```
exec-bit-policy    config    skip    opt    no staged files
```

**Disabled (default):**
```
exec-bit-policy    config    disabled    opt    CODERO_ENFORCE_EXECUTABLE_POLICY not set
```

---

## Running with Optional Checks

### One-time Activation

```bash
CODERO_ENFORCE_FORBIDDEN_PATHS=1 \
CODERO_FORBIDDEN_PATH_REGEX="(secrets/|\\.env$)" \
CODERO_ENFORCE_LOCKFILE_SYNC=1 \
codero gate-check
```

### Persistent Activation

Add to your shell profile or `.envrc` (if using direnv):

```bash
# ~/.bashrc or .envrc
export CODERO_ENFORCE_FORBIDDEN_PATHS=1
export CODERO_FORBIDDEN_PATH_REGEX="(secrets/|\\.env$|\\.pem$)"
export CODERO_ENFORCE_LOCKFILE_SYNC=1
export CODERO_ENFORCE_EXECUTABLE_POLICY=1
```

### CI/CD Integration

Set env vars in your CI pipeline before running the gate-check:

```yaml
# GitHub Actions example
env:
  CODERO_ENFORCE_FORBIDDEN_PATHS: "1"
  CODERO_FORBIDDEN_PATH_REGEX: "(secrets/|\\.env$)"
  CODERO_ENFORCE_LOCKFILE_SYNC: "1"
```

---

## Related Documentation

- **Env contract:** `docs/contracts/gate-check-env-contract-v1.md` — Full env variable reference
- **Schema contract:** `docs/contracts/gate-check-schema-v1.md` — Canonical JSON schema
- **Engine runbook:** `docs/runbooks/gate-check-engine.md` — Profile and check inventory