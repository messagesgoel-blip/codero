package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codero/codero/internal/gatecheck"
)

// ResolveGateCheckReportPath returns the canonical gate-check report path.
// CODERO_GATE_CHECK_REPORT_PATH wins; otherwise the default report path is
// resolved relative to repoRoot when provided and relative to cwd when empty.
func ResolveGateCheckReportPath(repoRoot string) string {
	if p := os.Getenv("CODERO_GATE_CHECK_REPORT_PATH"); p != "" {
		return p
	}
	if repoRoot == "" {
		return gatecheck.DefaultReportPath
	}
	return filepath.Join(repoRoot, gatecheck.DefaultReportPath)
}

// LoadGateCheckReport reads and parses the current gate-check report.
// A missing report is not treated as an error; the returned report will be nil.
func LoadGateCheckReport(repoRoot string) (*gatecheck.Report, string, error) {
	reportPath := ResolveGateCheckReportPath(repoRoot)
	data, err := os.ReadFile(reportPath) //nolint:gosec
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, reportPath, nil
		}
		return nil, reportPath, fmt.Errorf("read gate-check report: %w", err)
	}

	var report gatecheck.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, reportPath, fmt.Errorf("parse gate-check report: %w", err)
	}
	return &report, reportPath, nil
}

// LoadGateCheckReportData returns the raw JSON bytes for the current gate-check
// report. A missing report yields a nil payload and no error.
func LoadGateCheckReportData(repoRoot string) ([]byte, string, error) {
	reportPath := ResolveGateCheckReportPath(repoRoot)
	data, err := os.ReadFile(reportPath) //nolint:gosec
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, reportPath, nil
		}
		return nil, reportPath, fmt.Errorf("read gate-check report: %w", err)
	}
	return data, reportPath, nil
}

// LoadActiveSessions mirrors the canonical active-sessions dashboard query.
func LoadActiveSessions(ctx context.Context, db *sql.DB, limit int) ([]ActiveSession, error) {
	sessions, err := queryActiveSessions(ctx, db, limit)
	if err != nil {
		return nil, err
	}
	if sessions == nil {
		return []ActiveSession{}, nil
	}
	return sessions, nil
}

// LoadBlockReasons mirrors the canonical block-reasons dashboard query.
func LoadBlockReasons(ctx context.Context, db *sql.DB) ([]BlockReason, error) {
	reasons, err := queryBlockReasons(ctx, db)
	if err != nil {
		return nil, err
	}
	if reasons == nil {
		return []BlockReason{}, nil
	}
	return reasons, nil
}
