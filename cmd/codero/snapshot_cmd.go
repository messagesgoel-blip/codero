package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// dailySnapshotCmd automates the daily scorecard snapshot collection with
// retention enforcement and idempotent writes.
func dailySnapshotCmd(configPath *string) *cobra.Command {
	var (
		snapshotDir   string
		retainDays    int
		verifyOnly    bool
		skipPreflight bool
		reposFile     string
		toolsDir      string
	)

	cmd := &cobra.Command{
		Use:   "daily-snapshot",
		Short: "Collect and persist the daily proving snapshot with retention",
		Long: `Automated daily scorecard collection for Phase 1F evidence.

Steps executed on each run:
  1. Run preflight checks (tools, heartbeat, hook enforcement) — unless --skip-preflight
  2. Check if today's snapshot already exists in DB (idempotent: skip if present)
  3. Compute scorecard and save to DB and --snapshot-dir file
  4. Apply retention: remove files older than --retain-days days
  5. Print timestamped status lines for audit log capture

Use --verify-only to confirm today's snapshot exists without writing anything.

Exit codes:
  0  snapshot written or already present (idempotent)
  1  preflight failure, DB error, or snapshot directory unwritable
  2  verify-only mode: today's snapshot is MISSING

Examples:
  codero daily-snapshot --snapshot-dir /var/lib/codero/snapshots
  codero daily-snapshot --verify-only --snapshot-dir /var/lib/codero/snapshots
  codero daily-snapshot --retain-days 60 --snapshot-dir /mnt/audit/proving`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runDailySnapshot(cmd.Context(), *configPath, dailySnapshotOpts{
				snapshotDir:   snapshotDir,
				retainDays:    retainDays,
				verifyOnly:    verifyOnly,
				skipPreflight: skipPreflight,
				reposFile:     reposFile,
				toolsDir:      toolsDir,
			})
			if errors.Is(err, errSnapshotMissing) {
				os.Exit(snapshotExitCodeMissing)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&snapshotDir, "snapshot-dir", "",
		"directory to write snapshot JSON files (required for file retention)")
	cmd.Flags().IntVar(&retainDays, "retain-days", 45,
		"days of snapshot files to retain (0 = unlimited; DB rows are never deleted)")
	cmd.Flags().BoolVar(&verifyOnly, "verify-only", false,
		"verify today's snapshot exists; exit 2 if missing, do not write")
	cmd.Flags().BoolVar(&skipPreflight, "skip-preflight", false,
		"skip preflight dependency checks (not recommended in production)")
	cmd.Flags().StringVar(&reposFile, "repos-file", "docs/managed-repos.txt",
		"managed repos list for preflight hook check")
	cmd.Flags().StringVar(&toolsDir, "tools-dir", "/srv/storage/shared/tools/bin",
		"shared tools directory for preflight check")

	return cmd
}

type dailySnapshotOpts struct {
	snapshotDir   string
	retainDays    int
	verifyOnly    bool
	skipPreflight bool
	reposFile     string
	toolsDir      string
}

// snapshotExitCodeMissing is used by verify-only mode when snapshot is absent.
const snapshotExitCodeMissing = 2

var errSnapshotMissing = errors.New("snapshot missing")

func runDailySnapshot(ctx context.Context, configPath string, opts dailySnapshotOpts) error {
	ts := func() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }
	today := time.Now().UTC().Format("2006-01-02")

	fmt.Printf("[%s] daily-snapshot: starting for date %s\n", ts(), today)

	// 1. Preflight checks
	if !opts.skipPreflight {
		results, allPass := runPreflight(opts.reposFile, opts.toolsDir)
		if !allPass {
			fmt.Printf("[%s] daily-snapshot: preflight FAILED\n", ts())
			printPreflightResults(results)
			return fmt.Errorf("daily-snapshot: preflight checks failed — correct issues before collecting evidence")
		}
		fmt.Printf("[%s] daily-snapshot: preflight PASS (%d checks)\n", ts(), len(results))
	} else {
		fmt.Printf("[%s] daily-snapshot: preflight skipped (--skip-preflight)\n", ts())
	}

	// 2. Open DB
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("daily-snapshot: config: %w", err)
	}
	db, err := state.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("daily-snapshot: DB unavailable at %s: %w", cfg.DBPath, err)
	}
	defer db.Close()

	// 3. Verify snapshot dir writable (if provided)
	if opts.snapshotDir != "" {
		if err := os.MkdirAll(opts.snapshotDir, 0755); err != nil {
			return fmt.Errorf("daily-snapshot: cannot create snapshot directory %s: %w",
				opts.snapshotDir, err)
		}
		testFile := filepath.Join(opts.snapshotDir, ".write-check")
		if err := os.WriteFile(testFile, []byte("ok"), 0644); err != nil {
			return fmt.Errorf("daily-snapshot: snapshot directory not writable %s: %w",
				opts.snapshotDir, err)
		}
		os.Remove(testFile)
	}

	// 4. Verify-only mode
	if opts.verifyOnly {
		return runVerifyOnly(ctx, db, opts.snapshotDir, today, ts())
	}

	// 5. Idempotency: skip if today's snapshot already exists in DB
	exists, err := state.SnapshotExistsForDate(ctx, db, today)
	if err != nil {
		return fmt.Errorf("daily-snapshot: idempotency check: %w", err)
	}
	if exists {
		fmt.Printf("[%s] daily-snapshot: snapshot for %s already present — skipping write (idempotent)\n",
			ts(), today)
		return nil
	}

	// 6. Compute scorecard
	fmt.Printf("[%s] daily-snapshot: computing scorecard\n", ts())
	card, err := state.ComputeProvingScorecard(ctx, db)
	if err != nil {
		return fmt.Errorf("daily-snapshot: compute scorecard: %w", err)
	}

	cardJSON, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("daily-snapshot: marshal scorecard: %w", err)
	}

	// 7. Save to DB
	if err := state.SaveProvingSnapshot(ctx, db, today, string(cardJSON)); err != nil {
		return fmt.Errorf("daily-snapshot: save to DB: %w", err)
	}
	fmt.Printf("[%s] daily-snapshot: snapshot saved to DB for %s\n", ts(), today)

	// 8. Save to file (append-only: never overwrite existing dates)
	if opts.snapshotDir != "" {
		filePath := filepath.Join(opts.snapshotDir, today+".json")
		if _, err := os.Stat(filePath); err == nil {
			fmt.Printf("[%s] daily-snapshot: file already exists (append-only, keeping existing): %s\n",
				ts(), filePath)
		} else if os.IsNotExist(err) {
			if err := os.WriteFile(filePath, cardJSON, 0644); err != nil {
				return fmt.Errorf("daily-snapshot: write file %s: %w", filePath, err)
			}
			fmt.Printf("[%s] daily-snapshot: snapshot file written: %s\n", ts(), filePath)
		} else {
			return fmt.Errorf("daily-snapshot: stat file %s: %w", filePath, err)
		}

		// 9. Retention: remove files older than retainDays (DB rows preserved)
		if opts.retainDays > 0 {
			removed, err := applySnapshotRetention(opts.snapshotDir, opts.retainDays)
			if err != nil {
				// Retention failures are warnings only — do not fail the snapshot
				fmt.Fprintf(os.Stderr, "[%s] daily-snapshot: retention warning: %v\n", ts(), err)
			} else if removed > 0 {
				fmt.Printf("[%s] daily-snapshot: retention: removed %d file(s) older than %d days\n",
					ts(), removed, opts.retainDays)
			}
		}
	}

	fmt.Printf("[%s] daily-snapshot: DONE\n", ts())
	return nil
}

// runVerifyOnly checks whether today's snapshot exists in both DB and file.
func runVerifyOnly(ctx context.Context, db *state.DB, snapshotDir, today, ts string) error {
	dbExists, err := state.SnapshotExistsForDate(ctx, db, today)
	if err != nil {
		return fmt.Errorf("verify-only: DB check: %w", err)
	}

	fileExists := false
	if snapshotDir != "" {
		filePath := filepath.Join(snapshotDir, today+".json")
		if _, err := os.Stat(filePath); err == nil {
			fileExists = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("verify-only: stat file snapshot %s: %w", filePath, err)
		}
	}

	if dbExists {
		fmt.Printf("[%s] daily-snapshot: verify-only: DB snapshot for %s → PRESENT\n", ts, today)
	} else {
		fmt.Printf("[%s] daily-snapshot: verify-only: DB snapshot for %s → MISSING\n", ts, today)
	}

	if snapshotDir != "" {
		if fileExists {
			fmt.Printf("[%s] daily-snapshot: verify-only: file snapshot for %s → PRESENT\n", ts, today)
		} else {
			fmt.Printf("[%s] daily-snapshot: verify-only: file snapshot for %s → MISSING\n", ts, today)
		}
	}

	if !dbExists || (snapshotDir != "" && !fileExists) {
		return errSnapshotMissing
	}
	return nil
}

// applySnapshotRetention deletes JSON snapshot files older than retainDays days
// from snapshotDir. Returns the count of removed files.
// DB rows are never deleted by retention.
func applySnapshotRetention(snapshotDir string, retainDays int) (int, error) {
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return 0, fmt.Errorf("read snapshot dir: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -retainDays)
	removed := 0

	// Collect dated files in sorted order so we can safely remove oldest.
	var dated []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		datePart := strings.TrimSuffix(e.Name(), ".json")
		if t, err := time.Parse("2006-01-02", datePart); err == nil && t.Before(cutoff) {
			dated = append(dated, e.Name())
		}
	}
	sort.Strings(dated)

	for _, name := range dated {
		path := filepath.Join(snapshotDir, name)
		if err := os.Remove(path); err != nil {
			return removed, fmt.Errorf("remove %s: %w", path, err)
		}
		removed++
	}
	return removed, nil
}
