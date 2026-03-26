package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveGateCheckReportPath_UsesRepoRoot(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "codero-repo")
	got := ResolveGateCheckReportPath(repoRoot)
	want := filepath.Join(repoRoot, ".codero", "gate-check", "last-report.json")
	if got != want {
		t.Fatalf("ResolveGateCheckReportPath() = %q, want %q", got, want)
	}
}

func TestLoadGateCheckReport_MissingReturnsNil(t *testing.T) {
	report, reportPath, err := LoadGateCheckReport(t.TempDir())
	if err != nil {
		t.Fatalf("LoadGateCheckReport() error = %v", err)
	}
	if report != nil {
		t.Fatalf("LoadGateCheckReport() report = %#v, want nil", report)
	}
	if reportPath == "" {
		t.Fatal("LoadGateCheckReport() reportPath is empty")
	}
}

func TestLoadGateCheckReport_EnvOverrideWins(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "report.json")
	payload := `{"generated_at":"2026-03-25T00:00:00Z","summary":{"overall_status":"pass","passed":1,"failed":0,"skipped":0,"infra_bypassed":0,"disabled":0,"total":1,"required_failed":0,"required_disabled":0,"profile":"fast"},"checks":[{"id":"fmt","name":"gofmt","group":"style","status":"pass","required":true,"enabled":true,"duration_ms":12}]}`
	if err := os.WriteFile(override, []byte(payload), 0o600); err != nil {
		t.Fatalf("write override report: %v", err)
	}
	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", override)

	report, reportPath, err := LoadGateCheckReport(filepath.Join(dir, "ignored"))
	if err != nil {
		t.Fatalf("LoadGateCheckReport() error = %v", err)
	}
	if reportPath != override {
		t.Fatalf("reportPath = %q, want %q", reportPath, override)
	}
	if report == nil || report.Summary.Total != 1 {
		t.Fatalf("report = %#v, want parsed override report", report)
	}
}
