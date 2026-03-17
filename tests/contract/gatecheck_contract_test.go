package contract

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/codero/codero/internal/gatecheck"
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
