package contract

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/state"
)

func TestGateCheckSchemaContract(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(out))
	}

	var report gatecheck.Report
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode gate-check JSON: %v\noutput: %s", err, string(out))
	}

	if report.Summary.SchemaVersion != gatecheck.SchemaVersion {
		t.Fatalf("schema_version: got %q, want %q", report.Summary.SchemaVersion, gatecheck.SchemaVersion)
	}

	allowedStatus := map[gatecheck.CheckStatus]bool{
		gatecheck.StatusPass:     true,
		gatecheck.StatusFail:     true,
		gatecheck.StatusSkip:     true,
		gatecheck.StatusDisabled: true,
	}
	allowedReason := map[gatecheck.ReasonCode]bool{
		gatecheck.ReasonUserDisabled:   true,
		gatecheck.ReasonMissingTool:    true,
		gatecheck.ReasonNotApplicable:  true,
		gatecheck.ReasonNotInScope:     true,
		gatecheck.ReasonTimeout:        true,
		gatecheck.ReasonInfraBypass:    true,
		gatecheck.ReasonInfraAuth:      true,
		gatecheck.ReasonInfraRateLimit: true,
		gatecheck.ReasonInfraNetwork:   true,
		gatecheck.ReasonExecError:      true,
		"":                             true,
	}

	for _, c := range report.Checks {
		if !allowedStatus[c.Status] {
			t.Fatalf("check %q has unsupported status %q", c.ID, c.Status)
		}
		if !allowedReason[c.ReasonCode] {
			t.Fatalf("check %q has unsupported reason_code %q", c.ID, c.ReasonCode)
		}
		if (c.Status == gatecheck.StatusSkip || c.Status == gatecheck.StatusDisabled) && c.ReasonCode == "" {
			t.Fatalf("check %q status=%q must include reason_code", c.ID, c.Status)
		}
	}

	if report.Summary.OverallStatus != gatecheck.StatusPass && report.Summary.OverallStatus != gatecheck.StatusFail {
		t.Fatalf("overall_status: got %q, want pass or fail", report.Summary.OverallStatus)
	}

	wantOrder := []string{
		"file-size",
		"merge-markers",
		"trailing-whitespace",
		"final-newline",
		"forbidden-paths",
		"config-validation",
		"lockfile-sync",
		"exec-bit-policy",
		"gofmt",
		"gitleaks-staged",
		"semgrep",
		"ruff-lint",
		"ai-gate",
	}
	if len(report.Checks) != len(wantOrder) {
		t.Fatalf("checks length: got %d, want %d", len(report.Checks), len(wantOrder))
	}
	for i, id := range wantOrder {
		if report.Checks[i].ID != id {
			t.Fatalf("check order[%d]: got %q, want %q", i, report.Checks[i].ID, id)
		}
	}
}

func TestGateCheckSurfaceParity(t *testing.T) {
	root := repoRoot(t)
	fixtureDir := t.TempDir()
	reportPath := filepath.Join(fixtureDir, "last-report.json")

	jsonCmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off", "--report-path", reportPath)
	jsonCmd.Dir = root
	jsonOut, err := jsonCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(jsonOut))
	}

	var report gatecheck.Report
	if err := json.Unmarshal(jsonOut, &report); err != nil {
		t.Fatalf("decode gate-check json: %v\noutput: %s", err, string(jsonOut))
	}

	tuiCmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--tui-snapshot", "--profile", "off", "--report-path", reportPath)
	tuiCmd.Dir = root
	tuiOut, err := tuiCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --tui-snapshot --profile off failed: %v\noutput: %s", err, string(tuiOut))
	}
	snapshot := string(tuiOut)

	if !strings.Contains(snapshot, "GATE CHECKS") {
		t.Fatalf("tui snapshot missing header:\n%s", snapshot)
	}
	for _, check := range report.Checks {
		if check.Status == gatecheck.StatusPass {
			continue
		}
		if !strings.Contains(snapshot, check.ID) {
			t.Fatalf("tui snapshot missing check id %q:\n%s", check.ID, snapshot)
		}
		if check.ReasonCode != "" && !strings.Contains(snapshot, string(check.ReasonCode)) {
			t.Fatalf("tui snapshot missing reason_code %q for %q:\n%s", check.ReasonCode, check.ID, snapshot)
		}
		if check.Reason != "" && !strings.Contains(snapshot, check.Reason) {
			t.Fatalf("tui snapshot missing reason %q for %q:\n%s", check.Reason, check.ID, snapshot)
		}
	}

	db, err := state.Open(filepath.Join(fixtureDir, "dashboard.db"))
	if err != nil {
		t.Fatalf("open dashboard db: %v", err)
	}
	defer db.Close()

	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", reportPath)
	handler := dashboard.NewHandler(db.Unwrap(), dashboard.NewSettingsStore(fixtureDir))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/gate-checks", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard gate-checks status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Report     gatecheck.Report `json:"report"`
		ReportPath string           `json:"report_path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode dashboard payload: %v\nbody: %s", err, rec.Body.String())
	}
	if payload.ReportPath != reportPath {
		t.Fatalf("dashboard report_path = %q, want %q", payload.ReportPath, reportPath)
	}
	if !reflect.DeepEqual(payload.Report.Summary, report.Summary) {
		t.Fatalf("dashboard summary mismatch\nwant: %#v\ngot:  %#v", report.Summary, payload.Report.Summary)
	}
	if !reflect.DeepEqual(payload.Report.Checks, report.Checks) {
		t.Fatalf("dashboard checks mismatch\nwant: %#v\ngot:  %#v", report.Checks, payload.Report.Checks)
	}
	if payload.Report.RunAt.IsZero() {
		t.Fatalf("dashboard run_at should be populated")
	}
}
