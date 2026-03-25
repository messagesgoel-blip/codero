# Dashboard API v1 – Architecture & Certification Evidence

**Date:** 2026-03-24
**Status:** Certified
**Spec:** Dashboard API v1 (§3–§10, DA-1/4/5/9, AX-1/2/6/7)

---

## DA-1: Dashboard data from daemon state (no independent data store)

All dashboard endpoints read from the daemon's shared SQLite database (`state.Open()`).
The `Handler` struct accepts a `*sql.DB` injected from the daemon's state layer:

```go
type Handler struct {
    db       *sql.DB        // daemon's shared state DB
    settings *SettingsStore // JSON file backed by daemon data dir
}
```

No separate data store, cache, or replication is used. Every response is a
direct SQL query against the canonical daemon tables:
`agent_sessions`, `agent_assignments`, `agent_events`, `agent_rules`,
`assignment_rule_checks`, `branch_states`, `delivery_events`, `findings`,
`review_runs`, `activity_log`.

**Evidence:** `internal/dashboard/handlers.go` – `NewHandler()` constructor;
all query functions in `queries.go` and `dashboard_api_v1_queries.go` take `*sql.DB`.

---

## AX-2: Additive-only response changes

The v1 certification adds the `schema_version` field to all response structs.
No existing field has been removed or renamed.

Affected types (field added: `SchemaVersion string json:"schema_version"`):
- `OverviewResponse`
- `ActiveSessionsResponse`
- `AssignmentsResponse`
- `AgentEventsResponse`
- `ComplianceResponse`
- `SettingsResponse`
- `DashboardHealth`
- `GateCheckReport`
- `GateConfigResponse`

All new endpoint response types are new structs and do not modify existing ones.

**Evidence:** `git diff` of `models.go` and `handlers.go` shows only field additions.

---

## AX-6: No side effects beyond audit (read endpoints are pure reads)

All GET endpoints execute only SELECT queries against the daemon DB. They do not:
- Write to the database
- Modify files on disk
- Trigger daemon actions or external API calls
- Mutate in-memory state

The only write-path endpoints are:
- `PUT /settings` – writes to `dashboard-settings.json` (audit: logged)
- `PUT /settings/gate-config/{var}` – writes to `config.env` (audit: logged)
- `PUT /settings/repo-config/{repo}` – in-memory only (no persistence)
- `POST /merge/approve|reject|force` – acknowledgement only (no daemon mutation)
- `POST /manual-review-upload` – inserts a review_runs row (audit: logged)

**Evidence:** All handler functions in `handlers.go` and `dashboard_api_v1_handlers.go`;
query functions are read-only `SELECT` statements.

---

## Certification Matrix – Test Mapping

| Clause | Test Function(s) | Status |
|--------|-------------------|--------|
| §3 Sessions | `TestCert_S3_SessionsList`, `TestCert_S3_SessionDetail` | PASS |
| §4 Assignments | `TestCert_S4_AssignmentDetail`, `TestCert_S4_AssignmentDetail_WithRuleChecks` | PASS |
| §5 Feedback | `TestCert_S5_FeedbackByTaskID`, `TestCert_S5_FeedbackHistory` | PASS |
| §6 Gate | `TestCert_S6_GateLive`, `TestCert_S6_GateResults`, `TestCert_S6_GateFindings` | PASS |
| §7 Merge | `TestCert_S7_MergeStatus`, `TestCert_S7_MergeApprove`, `TestCert_S7_MergeReject` | PASS |
| §8 Settings | `TestCert_S8_RepoConfigGet`, `TestCert_S8_RepoConfigPut` | PASS |
| §9 Compliance | `TestCert_S9_ComplianceRules`, `TestCert_S9_ComplianceChecks`, `TestCert_S9_ComplianceViolations` | PASS |
| §10 Queue | `TestCert_S10_Queue`, `TestCert_S10_QueueStats` | PASS |
| DA-1 | This document (architecture evidence) | PASS |
| DA-4 | `TestCert_DA4_ForceMergeBlockedOnGateFail`, `TestCert_DA4_ForceMergeAllowedOnGatePass` | PASS |
| DA-5 | `TestCert_DA5_AlwaysOnCheckReturns403` | PASS |
| DA-9 | `TestCert_DA9_GateConfigWritesToConfigEnv` | PASS |
| AX-1 | `TestCert_AX1_AllEndpointsUnderPrefix` | PASS |
| AX-2 | This document (additive-only evidence) | PASS |
| AX-6 | This document (no side effects evidence) | PASS |
| AX-7 | `TestCert_AX7_SchemaVersion_*` (12 tests) | PASS |
