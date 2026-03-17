package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/codero/codero/internal/gatecheck"
)

// gateCheckCmd implements the `gate-check` subcommand: runs the local gate
// engine and reports all check statuses using the canonical model.
func gateCheckCmd() *cobra.Command {
	var (
		repoPath   string
		profile    string
		outputJSON bool
		reportPath string
		timeout    int
	)

	cmd := &cobra.Command{
		Use:   "gate-check",
		Short: "Run local pre-commit gate checks",
		Long: `Run the local pre-commit gate check engine (v2).

Reports the status of every check — pass, fail, skip, or disabled —
using the canonical check model. Disabled/skipped checks are always included in
output so engineers can see what is and is not running.

Profiles:
  strict    Missing required tools/checks => overall FAIL
  portable  Missing tools become DISABLED (not FAIL); only actual failures block
  off       Skip most checks; return PASS for local pipelines that cannot run tools

Environment variables (override flags):
  CODERO_GATES_PROFILE              Profile (strict|portable|off)
  CODERO_MAX_INFRA_BYPASS_GATES     Max infra-bypass before budget exceeded
  CODERO_ALLOW_REQUIRED_SKIP        Allow required checks to be disabled (1|0)
  CODERO_GATE_TIMEOUT               Per-engine timeout in seconds
  CODERO_MAX_STAGED_FILE_BYTES      Max size per staged file in bytes
  CODERO_ENFORCE_FORBIDDEN_PATHS    Enable forbidden path check (1|0)
  CODERO_FORBIDDEN_PATH_REGEX       Regex of forbidden paths
  CODERO_ENFORCE_LOCKFILE_SYNC      Enable lockfile sync check (1|0)
  CODERO_ENFORCE_EXECUTABLE_POLICY  Enable exec-bit policy check (1|0)
  CODERO_TOOL_GITLEAKS              Path to gitleaks binary
  CODERO_TOOL_SEMGREP               Path to semgrep binary
  CODERO_TOOL_RUFF                  Path to ruff binary
  CODERO_GATE_CHECK_REPORT_PATH     Persist report path (default: .codero/gate-check/last-report.json)

Exit codes:
  0  Overall PASS
  1  Overall FAIL or runtime error`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := gatecheck.LoadEngineConfig()

			// Flag overrides (flags win over env)
			if repoPath != "" {
				cfg.RepoPath = repoPath
			}
			if profile != "" {
				parsed, ok := parseGateCheckProfile(profile)
				if !ok {
					return fmt.Errorf("gate-check: unknown profile %q (want strict|portable|off; fast aliases portable)", profile)
				}
				cfg.Profile = parsed
			}
			if timeout > 0 {
				cfg.GateTimeout = time.Duration(timeout) * time.Second
			}

			ctx := cmd.Context()
			cancel := func() {}
			if cfg.GateTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, cfg.GateTimeout)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()

			engine := gatecheck.NewEngine(cfg)
			report := engine.Run(ctx)

			reportPath = resolveGateCheckReportPath(reportPath)
			if err := saveGateCheckReport(report, reportPath); err != nil {
				fmt.Fprintf(os.Stderr, "gate-check: warning: could not save report to %s: %v\n", reportPath, err)
			}

			if outputJSON {
				if err := writeGateCheckJSON(report); err != nil {
					return err
				}
			} else {
				printGateCheckTable(report)
			}

			if report.Summary.OverallStatus == gatecheck.StatusFail {
				return fmt.Errorf("gate-check: %d check(s) failed", report.Summary.Failed+report.Summary.RequiredFailed)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "", "path to repository root (default: cwd)")
	cmd.Flags().StringVarP(&profile, "profile", "p", "", "gate profile: strict|portable|off (fast aliases portable; default: from env or portable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "emit canonical JSON report to stdout")
	cmd.Flags().StringVar(&reportPath, "report-path", "", "write JSON report to this file (also: CODERO_GATE_CHECK_REPORT_PATH)")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "engine timeout in seconds (0 = use env/default)")

	return cmd
}

// writeGateCheckJSON serialises report as indented JSON to stdout.
func writeGateCheckJSON(report gatecheck.Report) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("gate-check: encode JSON: %w", err)
	}
	return nil
}

// printGateCheckTable writes a human-readable check table to stderr and a
// one-line summary + PASS/FAIL verdict to stdout.
func printGateCheckTable(report gatecheck.Report) {
	tw := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tGROUP\tSTATUS\tREQ\tTOOL\tREASON")
	fmt.Fprintln(tw, "──────────────────────\t──────────\t────────────\t───\t──────────\t──────────────────────────────────")
	for _, c := range report.Checks {
		req := "opt"
		if c.Required {
			req = "req"
		}
		reason := c.Reason
		if reason == "" && c.ReasonCode != "" {
			reason = string(c.ReasonCode)
		}
		tool := c.ToolName
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			c.ID, string(c.Group), string(c.Status), req, tool, reason)
	}
	tw.Flush()

	s := report.Summary
	fmt.Fprintf(os.Stderr, "\nSummary  pass=%d  fail=%d  skip=%d  infra=%d  disabled=%d  total=%d  profile=%s\n",
		s.Passed, s.Failed, s.Skipped, s.InfraBypassed, s.Disabled, s.Total, string(s.Profile))

	verdict := "✅ PASS"
	if s.OverallStatus == gatecheck.StatusFail {
		verdict = "❌ FAIL"
	}
	fmt.Printf("gate-check: %s\n", verdict)
}

func resolveGateCheckReportPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env, ok := os.LookupEnv("CODERO_GATE_CHECK_REPORT_PATH"); ok && env != "" {
		return env
	}
	return gatecheck.DefaultReportPath
}

func parseGateCheckProfile(raw string) (gatecheck.Profile, bool) {
	switch gatecheck.Profile(raw) {
	case gatecheck.ProfileStrict:
		return gatecheck.ProfileStrict, true
	case gatecheck.ProfilePortable:
		return gatecheck.ProfilePortable, true
	case gatecheck.ProfileOff:
		return gatecheck.ProfileOff, true
	case gatecheck.Profile("fast"):
		return gatecheck.ProfilePortable, true
	default:
		return "", false
	}
}

// saveGateCheckReport writes report as JSON to path, creating parent dirs as needed.
func saveGateCheckReport(report gatecheck.Report, path string) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	f, err := os.Create(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close report file: %w", cerr)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err = enc.Encode(report); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	return nil
}
