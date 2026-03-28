package contract

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/codero/codero/internal/gatecheck"
)

// TestLogDisplayStateNormalization validates LOG-001: every GC-001 check status
// maps to exactly one display state (passing/failing/disabled), and the mapping
// matches the contract table.
func TestLogDisplayStateNormalization(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off")
	cmd.Dir = root
	cmd.Env = cleanGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(out))
	}

	var report gatecheck.Report
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode gate-check JSON: %v\noutput: %s", err, string(out))
	}

	// LOG-001 §Canonical Display States: every status maps to one display state.
	wantMapping := map[gatecheck.CheckStatus]gatecheck.DisplayState{
		gatecheck.StatusPass:     gatecheck.DisplayPassing,
		gatecheck.StatusFail:     gatecheck.DisplayFailing,
		gatecheck.StatusSkip:     gatecheck.DisplayDisabled,
		gatecheck.StatusDisabled: gatecheck.DisplayDisabled,
	}

	for _, c := range report.Checks {
		ds := c.Status.ToDisplayState()

		// Assert display state is one of the three canonical values.
		switch ds {
		case gatecheck.DisplayPassing, gatecheck.DisplayFailing, gatecheck.DisplayDisabled:
			// ok
		default:
			t.Fatalf("check %q: ToDisplayState() = %q, not a canonical display state", c.ID, ds)
		}

		// Assert mapping matches the LOG-001 contract table.
		if want, ok := wantMapping[c.Status]; ok {
			if ds != want {
				t.Fatalf("check %q: status=%q → display=%q, want %q", c.ID, c.Status, ds, want)
			}
		} else {
			// Unknown status should fall to disabled.
			if ds != gatecheck.DisplayDisabled {
				t.Fatalf("check %q: unknown status %q should map to disabled, got %q", c.ID, c.Status, ds)
			}
		}
	}
}

// TestLogDisplayStateNotInWireFormat confirms display_state is NOT present in
// the GC-001 JSON wire format — it's computed client-side per LOG-001.
func TestLogDisplayStateNotInWireFormat(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off")
	cmd.Dir = root
	cmd.Env = cleanGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(out))
	}

	// Unmarshal to raw map to check field names.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("decode raw JSON: %v", err)
	}

	// Check each check object for display_state field.
	var checksRaw []map[string]json.RawMessage
	if err := json.Unmarshal(raw["checks"], &checksRaw); err != nil {
		t.Fatalf("decode checks array: %v", err)
	}
	for i, check := range checksRaw {
		if _, exists := check["display_state"]; exists {
			t.Fatalf("check[%d] contains display_state in JSON wire format (LOG-001 says it must NOT be present)", i)
		}
	}
}

// TestLogReasonPreservation confirms skip/disabled checks preserve reason_code
// per LOG-001 §Reason Preservation.
func TestLogReasonPreservation(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off")
	cmd.Dir = root
	cmd.Env = cleanGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(out))
	}

	var report gatecheck.Report
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("decode gate-check JSON: %v", err)
	}

	for _, c := range report.Checks {
		if c.Status == gatecheck.StatusSkip || c.Status == gatecheck.StatusDisabled {
			if c.ReasonCode == "" {
				t.Fatalf("check %q: status=%q but reason_code is empty (LOG-001 §Reason Preservation)", c.ID, c.Status)
			}
		}
	}
}
