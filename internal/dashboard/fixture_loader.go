package dashboard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FixtureSessionEntry describes one seeded active session for fixture mode.
// Fields mirror the branch_states columns used by queryActiveSessions.
// State must be one of the active-session states accepted by the dashboard API.
type FixtureSessionEntry struct {
	SessionID               string  `json:"session_id"`
	AgentID                 string  `json:"agent_id"` // optional; falls back to owner_agent for legacy fixtures
	Repo                    string  `json:"repo"`
	Branch                  string  `json:"branch"`
	Worktree                string  `json:"worktree,omitempty"`
	TaskID                  string  `json:"task_id,omitempty"`
	Mode                    string  `json:"mode,omitempty"`
	State                   string  `json:"state"`       // branch_states status (e.g. "submitted")
	PRNumber                int     `json:"pr_number"`   // optional; 0 = no PR (legacy branch_states only)
	OwnerAgent              string  `json:"owner_agent"` // optional display label (legacy)
	StartedAt               string  `json:"started_at,omitempty"`
	LastSeenAt              string  `json:"last_seen_at,omitempty"`
	LastProgressAt          string  `json:"last_progress_at,omitempty"`
	LastIOAt                string  `json:"last_io_at,omitempty"`
	ContextPressure         string  `json:"context_pressure,omitempty"`
	CompactCount            int     `json:"compact_count,omitempty"`
	InferredStatus          string  `json:"inferred_status,omitempty"`
	InferredStatusUpdatedAt string  `json:"inferred_status_updated_at,omitempty"`
	OutputMB                float64 `json:"output_mb,omitempty"`
}

// FixtureActivityEntry describes one seeded delivery event for fixture mode.
type FixtureActivityEntry struct {
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	EventType string `json:"event_type"` // e.g. "state_transition", "finding_bundle", "system"
	Payload   string `json:"payload"`    // JSON string
}

// FixtureRunEntry describes one seeded review run for fixture mode.
// This populates the review_runs table so the overview panel shows real metrics.
type FixtureRunEntry struct {
	ID         string `json:"id"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Provider   string `json:"provider"`
	Status     string `json:"status"` // e.g. "completed", "failed", "running"
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

// FixtureDirResult holds the optional report path resolved from a fixture directory.
type FixtureDirResult struct {
	// ReportPath is non-empty when the fixture directory contains report.json.
	ReportPath string
}

// LoadFixtureDir reads sessions.json and activity.json from dir (both optional)
// and seeds the provided database with the fixture data. It also checks for
// report.json and, if found, returns its path in the result.
//
// Missing fixture files are silently skipped. Malformed files return an error.
// This function is intended for use with --serve-fixture mode only.
func LoadFixtureDir(ctx context.Context, db *sql.DB, dir string) (FixtureDirResult, error) {
	var result FixtureDirResult

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return result, fmt.Errorf("fixture_loader: resolve dir %q: %w", dir, err)
	}

	// Check for report.json to surface its path to the caller.
	reportCandidate := filepath.Join(absDir, "report.json")
	if _, err := os.Stat(reportCandidate); err == nil {
		result.ReportPath = reportCandidate
	}

	// Seed sessions.
	sessionsFile := filepath.Join(absDir, "sessions.json")
	sessions, err := readJSONFile[[]FixtureSessionEntry](sessionsFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("fixture_loader: read sessions.json: %w", err)
	}
	if len(sessions) > 0 {
		if err := SeedFixtureSessions(ctx, db, sessions); err != nil {
			return result, fmt.Errorf("fixture_loader: seed sessions: %w", err)
		}
	}

	// Seed activity events.
	activityFile := filepath.Join(absDir, "activity.json")
	events, err := readJSONFile[[]FixtureActivityEntry](activityFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("fixture_loader: read activity.json: %w", err)
	}
	if len(events) > 0 {
		if err := SeedFixtureActivity(db, events); err != nil {
			return result, fmt.Errorf("fixture_loader: seed activity: %w", err)
		}
	}

	// Seed review runs.
	runsFile := filepath.Join(absDir, "runs.json")
	runs, err := readJSONFile[[]FixtureRunEntry](runsFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("fixture_loader: read runs.json: %w", err)
	}
	if len(runs) > 0 {
		if err := SeedFixtureRuns(ctx, db, runs); err != nil {
			return result, fmt.Errorf("fixture_loader: seed runs: %w", err)
		}
	}

	return result, nil
}

// SeedFixtureSessions inserts branch_state rows for each session entry.
// owner_session_last_seen is set to now so the sessions appear as active within
// the dashboard's SessionHeartbeatTTL window.
func SeedFixtureSessions(ctx context.Context, db *sql.DB, entries []FixtureSessionEntry) error {
	now := time.Now().UTC()
	useAgentSessions, err := tableExists(ctx, db, "agent_sessions")
	if err != nil {
		return fmt.Errorf("fixture_loader: check agent_sessions: %w", err)
	}
	for i, e := range entries {
		if e.SessionID == "" {
			return fmt.Errorf("fixture_loader: sessions[%d]: session_id is required", i)
		}
		agentID := strings.TrimSpace(e.AgentID)
		if agentID == "" {
			agentID = strings.TrimSpace(e.OwnerAgent)
		}
		if agentID == "" {
			agentID = e.SessionID
		}
		if useAgentSessions {
			startedAt, err := fixtureTimestampOrDefault(e.StartedAt, now)
			if err != nil {
				return fmt.Errorf("fixture_loader: sessions[%d]: parse started_at: %w", i, err)
			}
			lastSeenAt, err := fixtureTimestampOrDefault(e.LastSeenAt, now)
			if err != nil {
				return fmt.Errorf("fixture_loader: sessions[%d]: parse last_seen_at: %w", i, err)
			}
			lastProgressAt, err := fixtureOptionalTimestamp(e.LastProgressAt)
			if err != nil {
				return fmt.Errorf("fixture_loader: sessions[%d]: parse last_progress_at: %w", i, err)
			}
			lastIOAt, err := fixtureOptionalTimestamp(e.LastIOAt)
			if err != nil {
				return fmt.Errorf("fixture_loader: sessions[%d]: parse last_io_at: %w", i, err)
			}
			inferredStatusUpdatedAt, err := fixtureOptionalTimestamp(e.InferredStatusUpdatedAt)
			if err != nil {
				return fmt.Errorf("fixture_loader: sessions[%d]: parse inferred_status_updated_at: %w", i, err)
			}
			contextPressure := strings.TrimSpace(strings.ToLower(e.ContextPressure))
			switch contextPressure {
			case "normal", "warning", "critical":
				// valid
			case "":
				contextPressure = "normal"
			default:
				return fmt.Errorf("fixture_loader: sessions[%d]: invalid context_pressure %q (expected normal, warning, critical)", i, e.ContextPressure)
			}
			inferredStatus := strings.TrimSpace(e.InferredStatus)
			if inferredStatus == "" {
				inferredStatus = "unknown"
			}
			sessionPayload, err := json.Marshal(map[string]string{
				"mode":             e.Mode,
				"inferred_status":  inferredStatus,
				"context_pressure": contextPressure,
			})
			if err != nil {
				return fmt.Errorf("fixture_loader: marshal session event for %q: %w", e.SessionID, err)
			}
			assignmentID := fixtureAssignmentID(e.SessionID, e.Repo, e.Branch, e.TaskID)
			assignmentPayload, err := json.Marshal(map[string]string{
				"assignment_id": assignmentID,
				"repo":          e.Repo,
				"branch":        e.Branch,
				"worktree":      e.Worktree,
				"task_id":       e.TaskID,
			})
			if err != nil {
				return fmt.Errorf("fixture_loader: marshal assignment event %q: %w", assignmentID, err)
			}
			if e.OutputMB < 0 {
				return fmt.Errorf("fixture_loader: sessions[%d]: output_mb cannot be negative", i)
			}
			if e.CompactCount < 0 {
				return fmt.Errorf("fixture_loader: sessions[%d]: compact_count cannot be negative", i)
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("fixture_loader: begin tx for session %q: %w", e.SessionID, err)
			}
			_, err = tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO agent_sessions
					(session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, last_io_at,
					 context_pressure, compact_count, inferred_status, inferred_status_updated_at,
					 repo, branch, output_bytes, ended_at, end_reason)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,'')`,
				e.SessionID, agentID, e.Mode, startedAt, lastSeenAt, nullTimeValue(lastProgressAt), nullTimeValue(lastIOAt),
				contextPressure, e.CompactCount, inferredStatus, nullTimeValue(inferredStatusUpdatedAt),
				e.Repo, e.Branch, int64(e.OutputMB*1024*1024),
			)
			if err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("fixture_loader: insert agent session %q: %w", e.SessionID, err)
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agent_events (session_id, agent_id, event_type, payload)
				VALUES (?, ?, 'session_registered', ?)`,
				e.SessionID, agentID, string(sessionPayload),
			)
			if err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("fixture_loader: insert agent event for session %q: %w", e.SessionID, err)
			}
			if e.Repo == "" || e.Branch == "" {
				if err := tx.Commit(); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("fixture_loader: commit session %q: %w", e.SessionID, err)
				}
				continue
			}
			_, err = tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO agent_assignments
					(assignment_id, session_id, agent_id, repo, branch, worktree, task_id, started_at, ended_at, end_reason, superseded_by)
				VALUES (?,?,?,?,?,?,?,?,NULL,'',NULL)`,
				assignmentID, e.SessionID, agentID, e.Repo, e.Branch, e.Worktree, e.TaskID, now,
			)
			if err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("fixture_loader: insert agent assignment %q: %w", assignmentID, err)
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO agent_events (session_id, agent_id, event_type, payload)
				VALUES (?, ?, 'assignment_attached', ?)`,
				e.SessionID, agentID,
				string(assignmentPayload),
			)
			if err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("fixture_loader: insert assignment event %q: %w", assignmentID, err)
			}
			if err := tx.Commit(); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("fixture_loader: commit session+assignment %q: %w", e.SessionID, err)
			}
			// Tail-log seeding is best-effort: the session rows are already durable
			// after Commit, so a tail-log failure must not abort subsequent fixtures.
			if err := seedFixtureTailLog(e.SessionID, e.OutputMB); err != nil {
				fmt.Fprintf(os.Stderr, "fixture_loader: seed tail log for %q: %v (skipping)\n", e.SessionID, err)
			}
			continue
		}

		if e.Repo == "" {
			return fmt.Errorf("fixture_loader: sessions[%d]: repo is required", i)
		}
		if e.Branch == "" {
			return fmt.Errorf("fixture_loader: sessions[%d]: branch is required", i)
		}
		state := e.State
		if state == "" {
			state = "submitted"
		}
		id := fixtureBranchStateID(e.Repo, e.Branch)
		// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
		_, err := db.Exec(`
			INSERT OR REPLACE INTO branch_states
				(id, repo, branch, head_hash, state,
				 retry_count, max_retries, approved, ci_green,
				 pending_events, unresolved_threads,
				 owner_session_id, owner_session_last_seen,
				 queue_priority, submission_time, created_at, updated_at,
				 pr_number, owner_agent)
			VALUES (?,?,?,?,?,0,3,0,0,0,0,?,?,0,?,?,?,?,?)`,
			id, e.Repo, e.Branch, "fixture-head", state,
			e.SessionID, now,
			now, now, now,
			e.PRNumber, e.OwnerAgent,
		)
		if err != nil {
			return fmt.Errorf("fixture_loader: insert session %q: %w", e.SessionID, err)
		}
	}
	return nil
}

func fixtureBranchStateID(repo, branch string) string {
	sum := sha256.Sum256([]byte(repo + ":" + branch))
	return "fixture-bs-" + hex.EncodeToString(sum[:8])
}

func fixtureAssignmentID(sessionID, repo, branch, taskID string) string {
	sum := sha256.Sum256([]byte(sessionID + ":" + repo + ":" + branch + ":" + taskID))
	return "fixture-asg-" + hex.EncodeToString(sum[:8])
}

func fixtureTimestampOrDefault(raw string, fallback time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

func fixtureOptionalTimestamp(raw string) (sql.NullTime, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sql.NullTime{}, nil
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return sql.NullTime{}, err
	}
	return sql.NullTime{Time: ts.UTC(), Valid: true}, nil
}

func nullTimeValue(ts sql.NullTime) any {
	if !ts.Valid {
		return nil
	}
	return ts.Time
}

func seedFixtureTailLog(sessionID string, outputMB float64) error {
	if outputMB <= 0 {
		return nil
	}
	tailDir := strings.TrimSpace(os.Getenv("CODERO_TAIL_DIR"))
	if tailDir == "" {
		return nil
	}
	// Sanitize sessionID to prevent path traversal.
	// Mirrors the reader-side rules in queries.go.
	clean := filepath.Base(sessionID)
	if clean == "" || clean == "." || clean == ".." || clean != sessionID {
		return fmt.Errorf("invalid sessionID for tail log: %q (must not contain path separators)", sessionID)
	}
	if err := os.MkdirAll(tailDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", tailDir, err)
	}
	sizeBytes := int64(outputMB * 1024 * 1024)
	if sizeBytes <= 0 {
		sizeBytes = 1
	}
	logPath := filepath.Join(tailDir, clean+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", logPath, err)
	}
	if err := f.Truncate(sizeBytes); err != nil {
		_ = f.Close()
		return fmt.Errorf("truncate %s: %w", logPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", logPath, err)
	}
	return nil
}

// SeedFixtureActivity inserts delivery_event rows for each activity entry.
// Sequence numbers are assigned starting from the current maximum seq + 1.
func SeedFixtureActivity(db *sql.DB, entries []FixtureActivityEntry) error {
	// Read current max seq so we don't collide with any existing rows.
	var maxSeq int64
	row := db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM delivery_events`)
	if err := row.Scan(&maxSeq); err != nil {
		return fmt.Errorf("fixture_loader: read max seq: %w", err)
	}

	now := time.Now().UTC()
	for i, e := range entries {
		if e.Repo == "" {
			return fmt.Errorf("fixture_loader: activity[%d]: repo is required", i)
		}
		if e.Branch == "" {
			return fmt.Errorf("fixture_loader: activity[%d]: branch is required", i)
		}
		if e.EventType == "" {
			return fmt.Errorf("fixture_loader: activity[%d]: event_type is required", i)
		}
		payload := e.Payload
		if payload == "" {
			payload = "{}"
		}
		maxSeq++
		seq := maxSeq
		ts := now.Add(-time.Duration(len(entries)-i) * time.Minute)
		// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
		_, err := db.Exec(`
			INSERT OR IGNORE INTO delivery_events
				(seq, repo, branch, head_hash, event_type, payload, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			seq, e.Repo, e.Branch, "fixture-head",
			e.EventType, payload, ts,
		)
		if err != nil {
			return fmt.Errorf("fixture_loader: insert activity[%d]: %w", i, err)
		}
	}
	return nil
}

// readJSONFile reads and unmarshals a JSON file into T.
// Returns os.ErrNotExist (wrapped) when the file does not exist.
func readJSONFile[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return zero, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, fmt.Errorf("parse %q: %w", path, err)
	}
	return v, nil
}

// SeedFixtureRuns inserts review_run rows for each run entry.
// The created_at timestamp is set to now so runs appear in "today's runs" queries.
// The started_at and finished_at retain their fixture values for realistic duration display.
func SeedFixtureRuns(ctx context.Context, db *sql.DB, entries []FixtureRunEntry) error {
	now := time.Now().UTC()
	for i, e := range entries {
		if e.ID == "" {
			return fmt.Errorf("fixture_loader: runs[%d]: id is required", i)
		}
		if e.Repo == "" {
			return fmt.Errorf("fixture_loader: runs[%d]: repo is required", i)
		}
		if e.Branch == "" {
			return fmt.Errorf("fixture_loader: runs[%d]: branch is required", i)
		}
		if e.Provider == "" {
			return fmt.Errorf("fixture_loader: runs[%d]: provider is required", i)
		}
		if e.Status == "" {
			return fmt.Errorf("fixture_loader: runs[%d]: status is required", i)
		}
		if e.StartedAt == "" {
			return fmt.Errorf("fixture_loader: runs[%d]: started_at is required", i)
		}

		startedAt, err := time.Parse(time.RFC3339, e.StartedAt)
		if err != nil {
			return fmt.Errorf("fixture_loader: runs[%d]: parse started_at: %w", i, err)
		}

		var finishedAt sql.NullTime
		if e.FinishedAt != "" {
			fa, err := time.Parse(time.RFC3339, e.FinishedAt)
			if err != nil {
				return fmt.Errorf("fixture_loader: runs[%d]: parse finished_at: %w", i, err)
			}
			finishedAt = sql.NullTime{Time: fa, Valid: true}
		}

		headHash := "fixture-head-" + e.ID[:min(8, len(e.ID))]
		// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
		_, err = db.ExecContext(ctx, `
			INSERT OR REPLACE INTO review_runs
				(id, repo, branch, head_hash, provider, status, started_at, finished_at, error, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			e.ID, e.Repo, e.Branch, headHash, e.Provider, e.Status,
			startedAt, finishedAt, e.Error, now,
		)
		if err != nil {
			return fmt.Errorf("fixture_loader: insert run %q: %w", e.ID, err)
		}
	}
	return nil
}
