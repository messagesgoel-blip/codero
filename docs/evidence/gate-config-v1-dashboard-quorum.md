# Gate Config v1 — Certification Evidence (DOC)

Covers §5.1a/§5.1b dashboard integration and §7 AI quorum architecture
per `codero_certification_matrix_v1.md` §5.

## §5.1a — Dashboard reads config.env

**Implementation surface:** `internal/dashboard/handlers.go` — `handleGateConfig()`

The GET `/api/v1/dashboard/settings/gate-config` handler calls
`gate.ResolveEffective(gate.DefaultConfigFilePath())`, returning all 20
registry variables with current value, source, tier, and drift status.

**Evidence:**
- `TestGetGateConfig_DefaultsWhenNoFile` — GET returns all defaults when no file
- `TestGetGateConfig_ReflectsFileValues` — GET returns values written to config.env
- `TestGetGateConfig_DriftDetection` — GET surfaces env-vs-file drift

## §5.1b — Dashboard writes config.env

**Implementation surface:** `internal/dashboard/handlers.go` — `handleGateConfigVar()`

PUT `/api/v1/dashboard/settings/gate-config/{var_name}` validates the variable
name against the registry, validates the value, then calls
`gate.SaveConfigVar()` which uses atomic temp-file + rename.

**Evidence:**
- `TestPutGateConfigVar_UpdatesFile` — PUT persists to file; read-after-write coherent
- `TestPutGateConfigVar_UnknownVar` — 404 on unregistered variable
- `TestPutGateConfigVar_InvalidValue` — 422 on validation failure

## §7 — AI Quorum Architecture

**Config layer (Gate Config v1):**
- `CODERO_AI_QUORUM`, `CODERO_AI_BUDGET_SECONDS`, `CODERO_MIN_AI_GATES`,
  `CODERO_LITELLM_TIMEOUT`, `CODERO_COPILOT_TIMEOUT`, `CODERO_AI_MODEL`
  are defined in `gate.Registry` with `TierAISetting` tier.
- All are loaded, validated, persisted, and surfaced via the dashboard API.
- `gate.LoadConfigFrom()` reads `CODERO_AI_BUDGET_SECONDS` into
  `Config.GateTotalTimeoutSec`; `buildEnv()` propagates timeout settings to
  the gate-heartbeat subprocess.

**Enforcement layer (Review Gate v1 scope):**
- Quorum counting and gate ordering are enforced by the external
  `gate-heartbeat` script, which receives AI settings via process environment.
- This is architectural delegation, not a gap — Gate Config v1's scope is
  configuration management; Review Gate v1 owns execution.

**Evidence:**
- `TestCert_GCv1_S7_AISettingsRegistry` — all 6 AI vars present with correct defaults/tiers
- `TestCert_GCv1_S7_BudgetLoadedIntoConfig` — budget env var flows into Config struct
