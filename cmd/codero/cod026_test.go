package main

// cod026_test.go — Tests for COD-026 features:
//   - printGateStatusJSON (gate-status --json)
//   - resolveTheme / resolveInitialTab (codero tui flag parsing)
//   - portsCmd output correctness
//   - dashboardCmd --check endpoint validation (with a mock HTTP server)

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codero/codero/internal/daemon"
	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/tui"
)

// --- printGateStatusJSON ---

func TestPrintGateStatusJSON_Pass(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusPass,
		CopilotStatus: "pass",
		LiteLLMStatus: "pass",
		RunID:         "run-abc",
		Comments:      nil,
		ProgressBar:   "[! copilot:pass] [! litellm:pass]",
	}

	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr

	err := printGateStatusJSON(r)

	wr.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, rd) //nolint:errcheck

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}

	if got := out["status"]; got != "PASS" {
		t.Errorf("status: got %v, want PASS", got)
	}
	if got := out["run_id"]; got != "run-abc" {
		t.Errorf("run_id: got %v, want run-abc", got)
	}
	// comments must be an empty array, not null.
	comments, ok := out["comments"].([]any)
	if !ok || comments == nil {
		t.Errorf("comments: expected empty array, got %v", out["comments"])
	}
}

func TestPrintGateStatusJSON_FailWithComments(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusFail,
		CopilotStatus: "blocked",
		LiteLLMStatus: "blocked",
		Comments:      []string{"semgrep: dangerous pattern detected"},
	}

	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr

	err := printGateStatusJSON(r)

	wr.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, rd) //nolint:errcheck

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	if got := out["status"]; got != "FAIL" {
		t.Errorf("status: got %v, want FAIL", got)
	}
	comments, ok := out["comments"].([]any)
	if !ok || len(comments) != 1 {
		t.Errorf("expected 1 comment, got %v", out["comments"])
	}
}

func TestGateStatusJSON_ParityWithGateEndpoint(t *testing.T) {
	repoPath := t.TempDir()
	progressDir := filepath.Join(repoPath, ".codero", "gate-heartbeat")
	if err := os.MkdirAll(progressDir, 0o755); err != nil {
		t.Fatalf("mkdir progress dir: %v", err)
	}
	envContent := strings.Join([]string{
		"RUN_ID=run-parity-1",
		"STATUS=FAIL",
		"COPILOT_STATUS=blocked",
		"LITELLM_STATUS=pass",
		"CURRENT_GATE=local-first-pass",
		"COMMENTS=first blocker|second blocker",
		"ELAPSED_SEC=17",
		"PROGRESS_BAR=[! copilot:blocked] [! litellm:pass]",
		"UPDATED_AT=2026-03-16T10:58:00-0400",
	}, "\n")
	if err := os.WriteFile(filepath.Join(progressDir, "progress.env"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("write progress.env: %v", err)
	}

	t.Setenv("CODERO_REPO_PATH", repoPath)
	client := redis.New("127.0.0.1:0", "")
	queue := scheduler.NewQueue(client)
	slotCounter := scheduler.NewSlotCounter(client)
	obs := daemon.NewObservabilityServer(client, queue, slotCounter, nil, "127.0.0.1", "0", "/dashboard", "test")

	req := httptest.NewRequest(http.MethodGet, "/gate", nil)
	rec := httptest.NewRecorder()
	obs.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/gate status: got %d, want 200", rec.Code)
	}
	var gateOut map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gateOut); err != nil {
		t.Fatalf("unmarshal /gate JSON: %v", err)
	}

	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr
	if err := printGateStatusJSON(parseEnvToResult(envContent)); err != nil {
		t.Fatalf("printGateStatusJSON returned error: %v", err)
	}
	wr.Close()
	os.Stdout = orig
	var cliBuf bytes.Buffer
	io.Copy(&cliBuf, rd) //nolint:errcheck

	var cliOut map[string]any
	if err := json.Unmarshal(cliBuf.Bytes(), &cliOut); err != nil {
		t.Fatalf("unmarshal CLI JSON: %v", err)
	}

	cliCore := map[string]any{
		"status":         cliOut["status"],
		"copilot_status": cliOut["copilot_status"],
		"litellm_status": cliOut["litellm_status"],
		"current_gate":   cliOut["current_gate"],
		"run_id":         cliOut["run_id"],
		"comments":       cliOut["comments"],
		"progress_bar":   cliOut["progress_bar"],
	}
	gateCore := map[string]any{
		"status":         gateOut["status"],
		"copilot_status": gateOut["copilot_status"],
		"litellm_status": gateOut["litellm_status"],
		"current_gate":   gateOut["current_gate"],
		"run_id":         gateOut["run_id"],
		"comments":       gateOut["comments"],
		"progress_bar":   gateOut["progress_bar"],
	}
	if !reflect.DeepEqual(cliCore, gateCore) {
		t.Fatalf("CLI /gate parity mismatch\ncli=%v\ngate=%v", cliCore, gateCore)
	}
}

// --- resolveTheme ---

func TestResolveTheme_Dark(t *testing.T) {
	theme := resolveTheme("dark")
	if theme.Name != "dark" {
		t.Errorf("dark theme should have Name=dark, got %q", theme.Name)
	}
}

func TestResolveTheme_Dracula(t *testing.T) {
	theme := resolveTheme("dracula")
	if theme.Name != "dark" {
		t.Errorf("dracula should use DefaultTheme (Name=dark), got %q", theme.Name)
	}
}

func TestResolveTheme_System(t *testing.T) {
	theme := resolveTheme("system")
	if theme.Name != "dark" {
		t.Errorf("system should use DefaultTheme (Name=dark), got %q", theme.Name)
	}
}

func TestResolveTheme_Light(t *testing.T) {
	theme := resolveTheme("light")
	if theme.Name != "light" {
		t.Errorf("light should use AltTheme (Name=light), got %q", theme.Name)
	}
}

func TestResolveTheme_VSCode(t *testing.T) {
	theme := resolveTheme("vscode")
	if theme.Name != "light" {
		t.Errorf("vscode should use AltTheme (Name=light), got %q", theme.Name)
	}
}

// --- resolveInitialTab ---

func TestResolveInitialTab(t *testing.T) {
	tests := []struct {
		input string
		want  tui.Tab
	}{
		{"gate", tui.TabOutput},
		{"output", tui.TabOutput},
		{"events", tui.TabEvents},
		{"queue", tui.TabQueue},
		{"findings", tui.TabFindings},
		{"EVENTS", tui.TabEvents},
		{"", tui.TabOutput},
		{"unknown", tui.TabOutput},
	}
	for _, tt := range tests {
		if got := resolveInitialTab(tt.input); got != tt.want {
			t.Errorf("resolveInitialTab(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- dashboardCmd --check via mock HTTP server ---

func TestRunDashboardCheck_AllHealthy(t *testing.T) {
	var requestedPaths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Capture stdout.
	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr

	err := runDashboardCheck(ts.URL, "/dashboard")

	wr.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, rd) //nolint:errcheck

	if err != nil {
		t.Errorf("expected nil error for healthy server, got: %v", err)
	}
	if !strings.Contains(buf.String(), "All endpoints healthy") {
		t.Errorf("expected 'All endpoints healthy' in output, got: %s", buf.String())
	}
	wantPaths := []string{"/dashboard/", "/dashboard/api/v1/dashboard/overview", "/gate"}
	if !reflect.DeepEqual(requestedPaths, wantPaths) {
		t.Errorf("requested paths: got %v, want %v", requestedPaths, wantPaths)
	}
}

func TestRunDashboardCheck_ServerDown(t *testing.T) {
	// Reserve an ephemeral port, then immediately close the listener so the
	// address is guaranteed not to be listening when we make the request.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := "http://" + ln.Addr().String()
	ln.Close()

	err = runDashboardCheck(addr, "/dashboard")
	if err == nil {
		t.Error("expected error when server is down")
	}
	if !strings.Contains(err.Error(), "dashboard check") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunDashboardCheck_PartialFailure(t *testing.T) {
	// Serve 404 for the overview API endpoint.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "overview") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr

	err := runDashboardCheck(ts.URL, "/dashboard")

	wr.Close()
	os.Stdout = orig
	io.Copy(io.Discard, rd) //nolint:errcheck

	if err == nil {
		t.Error("expected error when overview API returns 404")
	}
}

func TestGateStatusCmd_JSONConflictsWithWatchAndLogs(t *testing.T) {
	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--json", "--watch"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for --json with --watch")
	}

	cmd = gateStatusCmd()
	cmd.SetArgs([]string{"--json", "--logs"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for --json with --logs")
	}
}

// --- portsCmd output ---

func TestPortsCmd_DefaultOutput(t *testing.T) {
	// Run portsCmd via its RunE with a non-existent config so it falls back to defaults.
	cmd := portsCmd(strPtr("nonexistent-codero.yaml"))

	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr

	err := cmd.RunE(cmd, nil)

	wr.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, rd) //nolint:errcheck

	// Error is printed to stderr; RunE should not return error (best-effort).
	if err != nil {
		t.Errorf("portsCmd returned unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"observability", "dashboard SPA", "8080", "/dashboard"} {
		if !strings.Contains(out, want) {
			t.Errorf("portsCmd output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestPortsCmd_WebhookConflictWarning(t *testing.T) {
	// Use environment variable to simulate a conflicting webhook port.
	// We can't inject a real config easily, but we can call portsCmd with a
	// real temp YAML that sets both ports to the same value.
	tmp := t.TempDir()
	cfgFile := tmp + "/conflict.yaml"
	content := `github_token: ghp_test
repos:
  - org/repo
observability_port: 9090
webhook:
  enabled: true
  port: 9090
  secret: hunter2
`
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := portsCmd(strPtr(cfgFile))

	origStdout := os.Stdout
	origStderr := os.Stderr
	rd, wr, _ := os.Pipe()
	os.Stdout = wr
	// Redirect stderr too so the WARNING doesn't pollute test output.
	rdE, wrE, _ := os.Pipe()
	os.Stderr = wrE

	_ = cmd.RunE(cmd, nil)

	wr.Close()
	wrE.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr

	var buf bytes.Buffer
	io.Copy(&buf, rd)        //nolint:errcheck
	io.Copy(io.Discard, rdE) //nolint:errcheck

	out := buf.String()
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "port conflict") {
		t.Errorf("expected conflict warning in output, got:\n%s", out)
	}
}

// strPtr is a helper that returns a pointer to a string literal.
func strPtr(s string) *string { return &s }
