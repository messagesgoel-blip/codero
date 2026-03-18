package contract

// Tests for codero-finish.sh finish-loop behaviour (COD-055 regression suite).
//
// These tests validate:
//   1. The shared-toolkit codero-finish.sh script contains the AUTOSTAGE_DENYLIST
//      guarding SCRATCHPAD.md (and other ephemeral files) from auto-staging.
//   2. The shared-toolkit codero-finish.sh script contains dual-channel CodeRabbit
//      completion detection (reviews[] + comments[]).
//   3. The _cr_body_is_final helper correctly classifies CodeRabbit completion
//      signals by invoking the function via a subprocess bash call on the script.
//
// Running these tests requires the shared-toolkit script to be present at the
// standard path. Tests skip gracefully if the environment is absent.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sharedToolkitScript = "/srv/storage/shared/agent-toolkit/bin/codero-finish.sh"

func finishScriptPath(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(sharedToolkitScript); err != nil {
		t.Skipf("codero-finish.sh not present at %s — skipping (env-specific)", sharedToolkitScript)
	}
	return sharedToolkitScript
}

// TestFinishLoopAutoStageDenylistContainsScratchpad ensures SCRATCHPAD.md is
// declared in the AUTOSTAGE_DENYLIST inside codero-finish.sh.
// Regression guard for COD-055 (iteration 2 auto-staged SCRATCHPAD.md).
func TestFinishLoopAutoStageDenylistContainsScratchpad(t *testing.T) {
	script := finishScriptPath(t)
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read codero-finish.sh: %v", err)
	}
	if !strings.Contains(string(content), `"SCRATCHPAD.md"`) {
		t.Errorf("codero-finish.sh AUTOSTAGE_DENYLIST must contain \"SCRATCHPAD.md\"; not found in script")
	}
	if !strings.Contains(string(content), "AUTOSTAGE_DENYLIST") {
		t.Errorf("codero-finish.sh must define AUTOSTAGE_DENYLIST; variable not found in script")
	}
}

// TestFinishLoopCodeRabbitDualChannelDetection ensures Phase 5 checks BOTH
// the reviews[] API and the comments[] API for CodeRabbit completion.
// Regression guard for COD-055 (Phase 5 timed out because completion arrived
// as a PR comment rather than a formal review object).
func TestFinishLoopCodeRabbitDualChannelDetection(t *testing.T) {
	script := finishScriptPath(t)
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read codero-finish.sh: %v", err)
	}
	body := string(content)

	if !strings.Contains(body, `--json reviews`) {
		t.Errorf("codero-finish.sh Phase 5 must poll --json reviews (formal review channel)")
	}
	if !strings.Contains(body, `--json comments`) {
		t.Errorf("codero-finish.sh Phase 5 must poll --json comments (summary comment channel)")
	}
	if !strings.Contains(body, "_cr_body_is_final") {
		t.Errorf("codero-finish.sh must define _cr_body_is_final helper for unified completion detection")
	}
}

// TestFinishLoopCRBodyIsFinalHelperLogic invokes the _cr_body_is_final bash
// helper directly via a subprocess and verifies expected classification.
// Regression guard for COD-055 — verifies the helper logic in isolation.
func TestFinishLoopCRBodyIsFinalHelperLogic(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available — skipping")
	}
	script := finishScriptPath(t)

	cases := []struct {
		name     string
		body     string
		wantFinal bool
	}{
		{
			name: "summary_comment_zero_actionable",
			body: "<!-- This is an auto-generated comment by CodeRabbit -->\n## Summary by CodeRabbit\n\n**Actionable comments posted: 0**\n",
			wantFinal: true,
		},
		{
			name: "summary_comment_nonzero_actionable",
			body: "<!-- This is an auto-generated comment by CodeRabbit -->\n## Summary by CodeRabbit\n\n**Actionable comments posted: 3**\n",
			wantFinal: true,
		},
		{
			name: "review_in_progress_placeholder",
			body: "CodeRabbit review in progress... I will post a summary when complete.",
			wantFinal: false,
		},
		{
			name: "walkthrough_marker",
			body: "<!-- walkthrough_start -->\n## Walkthrough\nSome changes were made.\n",
			wantFinal: true,
		},
		{
			name: "empty_body",
			body: "",
			wantFinal: false,
		},
	}

	// Build a small bash script that sources _cr_body_is_final and echoes result.
	// We extract the function using awk (same approach as the shell test suite).
	helperScript := filepath.Join(t.TempDir(), "cr_helper_test.sh")
	fnWork := filepath.Join(t.TempDir(), "cr_fn_work.sh")
	extractAndTest := `#!/usr/bin/env bash
set -uo pipefail
FINISH_SCRIPT="` + script + `"
FN_WORK="` + fnWork + `"
start_line=$(grep -n "^_cr_body_is_final()" "$FINISH_SCRIPT" | cut -d: -f1)
awk -v start="$start_line" '
  NR >= start {
    print
    for (i = 1; i <= length($0); i++) {
      c = substr($0, i, 1)
      if (c == "{") depth++
      else if (c == "}") {
        depth--
        if (depth == 0 && NR > start) { found=1; exit }
      }
    }
  }
' "$FINISH_SCRIPT" > "$FN_WORK"
source "$FN_WORK"
rm -f "$FN_WORK"
BODY="$1"
_cr_body_is_final "$BODY"
`
	if err := os.WriteFile(helperScript, []byte(extractAndTest), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", helperScript, tc.body)
			out, err := cmd.Output()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("run helper: %v", err)
				}
			}
			result := strings.TrimSpace(string(out))
			if exitCode != 0 && result == "" {
				result = "1"
			}
			gotFinal := result == "0"
			if gotFinal != tc.wantFinal {
				t.Errorf("_cr_body_is_final(%q) = %q (final=%v), want final=%v",
					tc.name, result, gotFinal, tc.wantFinal)
			}
		})
	}
}
