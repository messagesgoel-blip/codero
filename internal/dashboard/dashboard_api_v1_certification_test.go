package dashboard_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gate"
)

// ══════════════════════════════════════════════════════════════════════════
// Dashboard API v1 Certification Test Suite
//
// Each test function is tagged with the certification clause it evidences.
// Coverage of all 16 criteria is tracked in the companion evidence document
// at docs/evidence/dashboard-api-v1-architecture.md.
// ══════════════════════════════════════════════════════════════════════════

// seedRuleCheck inserts one assignment_rule_checks row.
func seedRuleCheck(t *testing.T, db *sql.DB, checkID, assignmentID, sessionID, ruleID string, violated bool) {
	t.Helper()
	v := 0
	if violated {
		v = 1
	}
	_, err := db.Exec(`INSERT INTO assignment_rule_checks
		(check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
		 result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by)
		VALUES (?,?,?,?,1,datetime('now'),?,?,?,?,NULL,'')`,
		checkID, assignmentID, sessionID, ruleID,
		boolResult(violated), v, "[]", "test detail")
	if err != nil {
		t.Fatalf("seedRuleCheck: %v", err)
	}
}

func boolResult(violated bool) string {
	if violated {
		return "fail"
	}
	return "pass"
}

// seedAgentRule inserts one agent_rules row.
func seedAgentRule(t *testing.T, db *sql.DB, ruleID, ruleName, ruleKind string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO agent_rules
		(rule_id, rule_name, rule_kind, description, enforcement,
		 violation_action, routing_target, rule_version, active)
		VALUES (?,?,?,?,?,?,?,1,1)`,
		ruleID, ruleName, ruleKind, "test rule", "block", `["log"]`, "reviewer")
	if err != nil {
		t.Fatalf("seedAgentRule: %v", err)
	}
}

// ── AX-7: schema_version in all responses ────────────────────────────────

func TestCert_AX7_SchemaVersion_Overview(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/overview", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_ActiveSessions(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_Assignments(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_AgentEvents(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/agent-events", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_Compliance(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/compliance", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_Settings(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/settings", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_Health(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_GateConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/settings/gate-config", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_Sessions(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/sessions", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_Queue(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/queue", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_QueueStats(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/queue/stats", nil)
	assertSchemaVersion(t, rec, "1")
}

func TestCert_AX7_SchemaVersion_FeedbackHistory(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/feedback/history", nil)
	assertSchemaVersion(t, rec, "1")
}

// ── AX-1: all endpoints under /api/v1/dashboard/ ────────────────────────

func TestCert_AX1_AllEndpointsUnderPrefix(t *testing.T) {
	endpoints := []string{
		"/api/v1/dashboard/overview",
		"/api/v1/dashboard/repos",
		"/api/v1/dashboard/runs",
		"/api/v1/dashboard/activity",
		"/api/v1/dashboard/block-reasons",
		"/api/v1/dashboard/gate-health",
		"/api/v1/dashboard/active-sessions",
		"/api/v1/dashboard/assignments",
		"/api/v1/dashboard/agent-events",
		"/api/v1/dashboard/compliance",
		"/api/v1/dashboard/health",
		"/api/v1/dashboard/settings",
		"/api/v1/dashboard/sessions",
		"/api/v1/dashboard/queue",
		"/api/v1/dashboard/queue/stats",
		"/api/v1/dashboard/feedback/history",
		"/api/v1/dashboard/compliance/rules",
		"/api/v1/dashboard/compliance/violations",
	}

	h, _ := newTestHandler(t)
	for _, ep := range endpoints {
		if !strings.HasPrefix(ep, "/api/v1/dashboard/") {
			t.Errorf("endpoint %q does not have /api/v1/dashboard/ prefix", ep)
		}
		rec := doRequest(t, h, http.MethodGet, ep, nil)
		if rec.Code == http.StatusNotFound {
			t.Errorf("endpoint %q returned 404 — not registered", ep)
		}
	}
}

// ── §3: Session endpoints ───────────────────────────────────────────────

func TestCert_S3_SessionsList(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-1", "agent-A", "cli", now.Add(-10*time.Minute), now)
	seedAgentSession(t, db, "sess-2", "agent-B", "daemon", now.Add(-5*time.Minute), now)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/sessions", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.SessionListResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total != 2 {
		t.Errorf("total: got %d, want 2", resp.Total)
	}
	if len(resp.Sessions) != 2 {
		t.Errorf("sessions count: got %d, want 2", len(resp.Sessions))
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S3_SessionsList_FilterStatus(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-active", "agent-A", "cli", now, now)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/sessions?status=active", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.SessionListResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 active session, got %d", resp.Total)
	}
	for _, s := range resp.Sessions {
		if s.Status != "active" {
			t.Errorf("session %s status: got %q, want active", s.SessionID, s.Status)
		}
	}
}

func TestCert_S3_SessionDetail(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-detail", "agent-X", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-1", "sess-detail", "agent-X", "acme/api", "main", "", "task-1", now)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/sessions/sess-detail", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.SessionDetailResponse
	mustUnmarshal(t, rec, &resp)
	if resp.SessionID != "sess-detail" {
		t.Errorf("session_id: got %q, want sess-detail", resp.SessionID)
	}
	if len(resp.Assignments) != 1 {
		t.Errorf("assignments: got %d, want 1", len(resp.Assignments))
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S3_SessionDetail_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/sessions/nonexistent", nil)
	assertStatus(t, rec, http.StatusNotFound)
}

// ── §4: Assignment detail ───────────────────────────────────────────────

func TestCert_S4_AssignmentDetail(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-a", "agent-1", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-detail", "sess-a", "agent-1", "acme/api", "feat/x", "", "task-2", now)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments/asgn-detail", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.AssignmentDetailResponse
	mustUnmarshal(t, rec, &resp)
	if resp.AssignmentID != "asgn-detail" {
		t.Errorf("assignment_id: got %q, want asgn-detail", resp.AssignmentID)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S4_AssignmentDetail_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments/nonexistent", nil)
	assertStatus(t, rec, http.StatusNotFound)
}

func TestCert_S4_AssignmentDetail_WithRuleChecks(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-rc", "agent-rc", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-rc", "sess-rc", "agent-rc", "acme/api", "feat/rc", "", "", now)
	seedAgentRule(t, db, "rule-1", "no-secrets", "security")
	seedRuleCheck(t, db, "chk-1", "asgn-rc", "sess-rc", "rule-1", false)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments/asgn-rc", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.AssignmentDetailResponse
	mustUnmarshal(t, rec, &resp)
	if len(resp.RuleChecks) != 1 {
		t.Errorf("rule_checks: got %d, want 1", len(resp.RuleChecks))
	}
}

// ── §5: Feedback endpoints ──────────────────────────────────────────────

func TestCert_S5_FeedbackByTaskID(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "run-fb", "acme/api", "feat/fb", "litellm", "completed", 10*time.Second)
	seedFinding(t, db, "run-fb", "acme/api", "feat/fb", "error", "semgrep")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/feedback/feat/fb", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.FeedbackResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 feedback item, got %d", resp.Total)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S5_FeedbackHistory(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "run-fh", "acme/api", "main", "litellm", "completed", 5*time.Second)
	seedFinding(t, db, "run-fh", "acme/api", "main", "warning", "ruff")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/feedback/history", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.FeedbackHistoryResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 history item, got %d", resp.Total)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

// ── §6: Gate endpoints ──────────────────────────────────────────────────

func TestCert_S6_GateLive(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-gate", "agent-g", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-gate", "sess-gate", "agent-g", "acme/api", "main", "", "", now)
	seedRun(t, db, "run-live", "acme/api", "main", "litellm", "running", 0)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate/live/sess-gate", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.GateLiveResponse
	mustUnmarshal(t, rec, &resp)
	if resp.SessionID != "sess-gate" {
		t.Errorf("session_id: got %q", resp.SessionID)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S6_GateLive_IdleWhenNoRuns(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate/live/nonexistent", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.GateLiveResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Status != "idle" {
		t.Errorf("status: got %q, want idle", resp.Status)
	}
}

func TestCert_S6_GateResults(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-gr", "agent-gr", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-gr", "sess-gr", "agent-gr", "acme/api", "main", "", "", now)
	seedRun(t, db, "run-gr", "acme/api", "main", "litellm", "completed", 30*time.Second)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate/results/sess-gr", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.GateResultsResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 result, got %d", resp.Total)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S6_GateFindings(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "run-gf", "acme/api", "feat/gf", "semgrep", "completed", 10*time.Second)
	seedFinding(t, db, "run-gf", "acme/api", "feat/gf", "error", "semgrep")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate/findings/feat/gf", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.GateFindingsResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 finding, got %d", resp.Total)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

// ── §7: Merge endpoints ────────────────────────────────────────────────

func TestCert_S7_MergeStatus(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-ms", "agent-ms", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-ms", "sess-ms", "agent-ms", "acme/api", "main", "", "", now)
	seedRun(t, db, "run-ms", "acme/api", "main", "litellm", "completed", 10*time.Second)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/merge/status/asgn-ms", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.MergeStatusResponse
	mustUnmarshal(t, rec, &resp)
	if resp.AssignmentID != "asgn-ms" {
		t.Errorf("assignment_id: got %q", resp.AssignmentID)
	}
	if !resp.MergeEligible {
		t.Error("expected merge_eligible=true when gate passed")
	}
	if resp.GateStatus != "passed" {
		t.Errorf("gate_status: got %q, want passed", resp.GateStatus)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S7_MergeApprove(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-ma", "agent-ma", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-ma", "sess-ma", "agent-ma", "acme/api", "main", "", "", now)

	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/merge/approve/asgn-ma", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.MergeActionResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Action != "approve" {
		t.Errorf("action: got %q, want approve", resp.Action)
	}
}

func TestCert_S7_MergeReject(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-mr", "agent-mr", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-mr", "sess-mr", "agent-mr", "acme/api", "main", "", "", now)

	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/merge/reject/asgn-mr", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.MergeActionResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Action != "reject" {
		t.Errorf("action: got %q, want reject", resp.Action)
	}
}

// ── DA-4: RULE-001 force-merge blocked when gate failed ─────────────────

func TestCert_DA4_ForceMergeBlockedOnGateFail(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-da4", "agent-da4", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-da4", "sess-da4", "agent-da4", "acme/api", "feat/block", "", "", now)
	seedRun(t, db, "run-da4", "acme/api", "feat/block", "litellm", "failed", 10*time.Second)

	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/merge/force/asgn-da4", nil)
	assertStatus(t, rec, http.StatusForbidden)

	var errResp dashboard.ErrorResponse
	mustUnmarshal(t, rec, &errResp)
	if !strings.Contains(errResp.Error, "RULE-001") {
		t.Errorf("error should mention RULE-001, got: %s", errResp.Error)
	}
	if errResp.Code != "da4_gate_failed" {
		t.Errorf("code: got %q, want da4_gate_failed", errResp.Code)
	}
}

func TestCert_DA4_ForceMergeAllowedOnGatePass(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-da4p", "agent-da4p", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-da4p", "sess-da4p", "agent-da4p", "acme/api", "feat/pass", "", "", now)
	seedRun(t, db, "run-da4p", "acme/api", "feat/pass", "litellm", "completed", 10*time.Second)

	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/merge/force/asgn-da4p", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.MergeActionResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Action != "force" {
		t.Errorf("action: got %q, want force", resp.Action)
	}
}

// ── DA-5: always-on checks not disableable ──────────────────────────────

func TestCert_DA5_AlwaysOnCheckReturns403(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	for _, checkName := range gate.AlwaysOnChecks() {
		envName := "CODERO_" + strings.ToUpper(strings.ReplaceAll(checkName, "-", "_")) + "_ENABLED"
		body := `{"value":"false"}`
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/dashboard/settings/gate-config/"+envName,
			bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("DA-5: PUT %s: got %d, want 403; body: %s", envName, w.Code, w.Body.String())
		}

		var errResp dashboard.ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &errResp); err == nil {
			if errResp.Code != "always_on" {
				t.Errorf("DA-5: PUT %s: code=%q, want always_on", envName, errResp.Code)
			}
		}
	}
}

// ── DA-9: Settings write to config.env ──────────────────────────────────

func TestCert_DA9_GateConfigWritesToConfigEnv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"value":"true"}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/dashboard/settings/gate-config/CODERO_COPILOT_ENABLED",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT gate-config: %d; %s", w.Code, w.Body.String())
	}

	cfgPath := filepath.Join(tmpHome, ".codero", "config.env")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config.env not created: %v", err)
	}
	if !bytes.Contains(data, []byte("CODERO_COPILOT_ENABLED=true")) {
		t.Errorf("config.env missing value: %s", string(data))
	}
}

// ── §8: Settings/repo-config ────────────────────────────────────────────

func TestCert_S8_RepoConfigGet(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/settings/repo-config/acme/api", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.RepoConfigResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Repo != "acme/api" {
		t.Errorf("repo: got %q", resp.Repo)
	}
	if resp.Config["pr_auto_create"] == "" {
		t.Error("expected pr_auto_create in config")
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S8_RepoConfigPut(t *testing.T) {
	h, _ := newTestHandler(t)
	body := `{"coderabbit_opt_in":"true"}`
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/settings/repo-config/acme/api",
		bytes.NewBufferString(body))
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.RepoConfigResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Config["coderabbit_opt_in"] != "true" {
		t.Errorf("coderabbit_opt_in: got %q, want true", resp.Config["coderabbit_opt_in"])
	}
}

// ── §9: Compliance sub-endpoints ────────────────────────────────────────

func TestCert_S9_ComplianceRules(t *testing.T) {
	h, db := newTestHandler(t)
	seedAgentRule(t, db, "rule-sec", "secret-scan", "security")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/compliance/rules", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.ComplianceRulesResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 rule, got %d", resp.Total)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S9_ComplianceChecks(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-cc", "agent-cc", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-cc", "sess-cc", "agent-cc", "acme/api", "main", "", "", now)
	seedAgentRule(t, db, "rule-cc", "lint", "quality")
	seedRuleCheck(t, db, "chk-cc", "asgn-cc", "sess-cc", "rule-cc", false)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/compliance/checks/asgn-cc", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.ComplianceChecksResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 check, got %d", resp.Total)
	}
	if resp.AssignmentID != "asgn-cc" {
		t.Errorf("assignment_id: got %q", resp.AssignmentID)
	}
}

func TestCert_S9_ComplianceViolations(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()
	seedAgentSession(t, db, "sess-cv", "agent-cv", "cli", now, now)
	seedAgentAssignment(t, db, "asgn-cv", "sess-cv", "agent-cv", "acme/api", "main", "", "", now)
	seedAgentRule(t, db, "rule-cv", "secret-leak", "security")
	seedRuleCheck(t, db, "chk-cv", "asgn-cv", "sess-cv", "rule-cv", true)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/compliance/violations", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.ComplianceViolationsResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 1 {
		t.Errorf("expected at least 1 violation, got %d", resp.Total)
	}
}

// ── §10: Queue endpoints ────────────────────────────────────────────────

func TestCert_S10_Queue(t *testing.T) {
	h, db := newTestHandler(t)
	seedBranch(t, db, "acme/api", "feat/q1", "coding")
	seedBranch(t, db, "acme/api", "feat/q2", "queued_cli")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/queue", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.QueueResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 2 {
		t.Errorf("expected at least 2 queue items, got %d", resp.Total)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

func TestCert_S10_QueueStats(t *testing.T) {
	h, db := newTestHandler(t)
	seedBranch(t, db, "acme/api", "feat/s1", "queued_cli")
	seedBranch(t, db, "acme/api", "feat/s2", "coding")
	seedBranch(t, db, "acme/api", "feat/s3", "blocked")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/queue/stats", nil)
	assertStatus(t, rec, http.StatusOK)

	var resp dashboard.QueueStatsResponse
	mustUnmarshal(t, rec, &resp)
	if resp.Total < 3 {
		t.Errorf("total: got %d, want at least 3", resp.Total)
	}
	if resp.Pending < 1 {
		t.Errorf("pending: got %d, want at least 1", resp.Pending)
	}
	if resp.Active < 1 {
		t.Errorf("active: got %d, want at least 1", resp.Active)
	}
	if resp.Blocked < 1 {
		t.Errorf("blocked: got %d, want at least 1", resp.Blocked)
	}
	if resp.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q, want 1", resp.SchemaVersion)
	}
}

// ── Test helpers ─────────────────────────────────────────────────────────

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status: got %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func assertSchemaVersion(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v; body: %s", err, rec.Body.String())
	}
	sv, ok := raw["schema_version"]
	if !ok {
		t.Fatalf("schema_version field missing from response: %s", rec.Body.String())
	}
	if sv != want {
		t.Errorf("schema_version: got %v, want %q", sv, want)
	}
}

func mustUnmarshal(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("unmarshal: %v; body: %s", err, rec.Body.String())
	}
}
