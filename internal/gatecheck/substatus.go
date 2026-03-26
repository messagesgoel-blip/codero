package gatecheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultSubstatusPath is where gate-substatus.env is written.
	DefaultSubstatusPath = ".codero/gate-check/gate-substatus.env"
	// HeartbeatSubstatusPath is the pipeline heartbeat location.
	HeartbeatSubstatusPath = ".codero/gate-heartbeat/gate-substatus.env"
)

// WriteSubstatus atomically writes gate-substatus.env from a completed Report.
// The file is written to a temp file first, then renamed to ensure atomicity.
func WriteSubstatus(path string, report Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create substatus directory: %w", err)
	}

	result := "pass"
	if report.Summary.OverallStatus == StatusFail {
		result = "fail"
	}

	blocked := report.Summary.Failed > 0 || report.Summary.RequiredFailed > 0

	totalFindings := 0
	for _, c := range report.Checks {
		totalFindings += c.FindingsCount
	}

	invocation := report.Invocation
	if invocation == "" {
		invocation = "hook"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CODERO_GATE_RESULT=%s\n", result)
	fmt.Fprintf(&b, "CODERO_GATE_TIMESTAMP=%s\n", report.RunAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "CODERO_GATE_DURATION_MS=%d\n", totalDurationMS(report))
	fmt.Fprintf(&b, "CODERO_GATE_FINDINGS_COUNT=%d\n", totalFindings)
	fmt.Fprintf(&b, "CODERO_GATE_BLOCKED=%t\n", blocked)
	fmt.Fprintf(&b, "CODERO_GATE_INVOCATION=%s\n", invocation)

	for _, c := range report.Checks {
		key := strings.ToUpper(strings.ReplaceAll(c.ID, "-", "_"))
		fmt.Fprintf(&b, "CODERO_CHECK_%s_STATUS=%s\n", key, string(c.Status))
		fmt.Fprintf(&b, "CODERO_CHECK_%s_DURATION_MS=%d\n", key, c.DurationMS)
		fmt.Fprintf(&b, "CODERO_CHECK_%s_FINDINGS_COUNT=%d\n", key, c.FindingsCount)
		fmt.Fprintf(&b, "CODERO_CHECK_%s_EXIT_CODE=%d\n", key, c.ExitCode)
	}

	// Atomic write: temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write substatus temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename substatus: %w", err)
	}
	return nil
}

// totalDurationMS sums all check durations from the report.
func totalDurationMS(r Report) int64 {
	var total int64
	for _, c := range r.Checks {
		total += c.DurationMS
	}
	return total
}

// EnforceFindingsCap applies the MaxFindingsPerCheck cap to a slice of checks.
// It sets FindingsCount from the detail line count and marks Truncated if capped.
func EnforceFindingsCap(checks []CheckResult) {
	for i := range checks {
		if checks[i].Details == "" {
			continue
		}
		lines := strings.Split(checks[i].Details, "\n")
		count := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				count++
			}
		}
		if checks[i].FindingsCount == 0 && count > 0 {
			checks[i].FindingsCount = count
		}
		if checks[i].FindingsCount > MaxFindingsPerCheck {
			checks[i].Truncated = true
			// Truncate details to MaxFindingsPerCheck non-empty lines.
			var kept []string
			for _, l := range lines {
				if strings.TrimSpace(l) != "" {
					kept = append(kept, l)
					if len(kept) >= MaxFindingsPerCheck {
						break
					}
				}
			}
			checks[i].Details = strings.Join(kept, "\n")
		}
	}
}
