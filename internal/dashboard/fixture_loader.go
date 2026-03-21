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
	SessionID  string `json:"session_id"`
	AgentID    string `json:"agent_id"` // optional; falls back to owner_agent for legacy fixtures
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Worktree   string `json:"worktree,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	Mode       string `json:"mode,omitempty"`
	State      string `json:"state"`       // legacy branch_states status (e.g. "coding")
	PRNumber   int    `json:"pr_number"`   // optional; 0 = no PR (legacy branch_states only)
	OwnerAgent string `json:"owner_agent"` // optional display label (legacy)
}

// FixtureActivityEntry describes one seeded delivery event for fixture mode.
type FixtureActivityEntry struct {
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	EventType string `json:"event_type"` // e.g. "state_transition", "finding_bundle", "system"
	Payload   string `json:"payload"`    // JSON string
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
func LoadFixtureDir(db *sql.DB, dir string) (FixtureDirResult, error) {
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
		if err := SeedFixtureSessions(db, sessions); err != nil {
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

	return result, nil
}

// SeedFixtureSessions inserts branch_state rows for each session entry.
// owner_session_last_seen is set to now so the sessions appear as active within
// the dashboard's SessionHeartbeatTTL window.
func SeedFixtureSessions(db *sql.DB, entries []FixtureSessionEntry) error {
	now := time.Now().UTC()
	useAgentSessions, err := tableExists(context.Background(), db, "agent_sessions")
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
			// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
			_, err := db.Exec(`
				INSERT OR REPLACE INTO agent_sessions
					(session_id, agent_id, mode, started_at, last_seen_at, ended_at, end_reason)
				VALUES (?,?,?,?,?,NULL,'')`,
				e.SessionID, agentID, e.Mode, now, now,
			)
			if err != nil {
				return fmt.Errorf("fixture_loader: insert agent session %q: %w", e.SessionID, err)
			}
			if e.Repo == "" || e.Branch == "" {
				continue
			}
			assignmentID := fixtureAssignmentID(e.SessionID, e.Repo, e.Branch, e.TaskID)
			// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
			_, err = db.Exec(`
				INSERT OR REPLACE INTO agent_assignments
					(assignment_id, session_id, agent_id, repo, branch, worktree, task_id, started_at, ended_at, end_reason, superseded_by)
				VALUES (?,?,?,?,?,?,?,?,NULL,'',NULL)`,
				assignmentID, e.SessionID, agentID, e.Repo, e.Branch, e.Worktree, e.TaskID, now,
			)
			if err != nil {
				return fmt.Errorf("fixture_loader: insert agent assignment %q: %w", assignmentID, err)
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
			state = "coding"
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
