package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// BranchRecord is a full row from branch_states.
type BranchRecord struct {
	ID                   string
	Repo                 string
	Branch               string
	HeadHash             string
	State                State
	RetryCount           int
	MaxRetries           int
	Approved             bool
	CIGreen              bool
	PendingEvents        int
	UnresolvedThreads    int
	OwnerSessionID       string
	OwnerSessionLastSeen *time.Time
	QueuePriority        int
	SubmissionTime       *time.Time
	LeaseID              *string
	LeaseExpiresAt       *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// DeliveryEvent is a row from delivery_events.
type DeliveryEvent struct {
	Seq       int64
	Repo      string
	Branch    string
	HeadHash  string
	EventType string
	Payload   string
	CreatedAt time.Time
}

// ReviewRun is a row from review_runs.
type ReviewRun struct {
	ID         string
	Repo       string
	Branch     string
	HeadHash   string
	Provider   string
	Status     string // "pending", "running", "completed", "failed"
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      string
	CreatedAt  time.Time
}

// FindingRecord is a row from findings.
type FindingRecord struct {
	ID        string
	RunID     string
	Repo      string
	Branch    string
	Severity  string
	Category  string
	File      string
	Line      int
	Message   string
	Source    string
	RuleID    string
	Timestamp time.Time
	CreatedAt time.Time
}

// ErrBranchNotFound is returned when a branch record does not exist.
var ErrBranchNotFound = errors.New("branch not found")

// GetBranch retrieves a branch record by repo and branch name.
func GetBranch(db *DB, repo, branch string) (*BranchRecord, error) {
	const q = `
		SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
		       approved, ci_green, pending_events, unresolved_threads,
		       owner_session_id, owner_session_last_seen,
		       queue_priority, submission_time, lease_id, lease_expires_at,
		       created_at, updated_at
		FROM branch_states
		WHERE repo = ? AND branch = ?`

	row := db.sql.QueryRow(q, repo, branch)
	return scanBranch(row)
}

// GetBranchByID retrieves a branch record by primary key.
func GetBranchByID(db *DB, id string) (*BranchRecord, error) {
	const q = `
		SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
		       approved, ci_green, pending_events, unresolved_threads,
		       owner_session_id, owner_session_last_seen,
		       queue_priority, submission_time, lease_id, lease_expires_at,
		       created_at, updated_at
		FROM branch_states
		WHERE id = ?`

	row := db.sql.QueryRow(q, id)
	return scanBranch(row)
}

// ListBranchesByState returns all branches in the given state.
func ListBranchesByState(db *DB, st State) ([]BranchRecord, error) {
	const q = `
		SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
		       approved, ci_green, pending_events, unresolved_threads,
		       owner_session_id, owner_session_last_seen,
		       queue_priority, submission_time, lease_id, lease_expires_at,
		       created_at, updated_at
		FROM branch_states
		WHERE state = ?
		ORDER BY updated_at ASC`

	rows, err := db.sql.Query(q, string(st))
	if err != nil {
		return nil, fmt.Errorf("list branches by state: %w", err)
	}
	defer rows.Close()
	return scanBranches(rows)
}

// ListActiveBranches returns all branches in active states (non-terminal, non-abandoned).
func ListActiveBranches(db *DB) ([]BranchRecord, error) {
	const q = `
		SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
		       approved, ci_green, pending_events, unresolved_threads,
		       owner_session_id, owner_session_last_seen,
		       queue_priority, submission_time, lease_id, lease_expires_at,
		       created_at, updated_at
		FROM branch_states
		WHERE state IN ('coding','local_review','queued_cli','cli_reviewing','reviewed','merge_ready')
		ORDER BY updated_at ASC`

	rows, err := db.sql.Query(q)
	if err != nil {
		return nil, fmt.Errorf("list active branches: %w", err)
	}
	defer rows.Close()
	return scanBranches(rows)
}

// ListExpiredSessions returns active branches whose owner_session_last_seen
// has passed the given TTL threshold and are eligible for T14 (→ abandoned).
func ListExpiredSessions(db *DB, ttl time.Duration) ([]BranchRecord, error) {
	threshold := time.Now().Add(-ttl)
	const q = `
		SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
		       approved, ci_green, pending_events, unresolved_threads,
		       owner_session_id, owner_session_last_seen,
		       queue_priority, submission_time, lease_id, lease_expires_at,
		       created_at, updated_at
		FROM branch_states
		WHERE state IN ('coding','local_review','queued_cli','cli_reviewing','reviewed','merge_ready')
		  AND owner_session_last_seen IS NOT NULL
		  AND owner_session_last_seen < ?
		ORDER BY owner_session_last_seen ASC`

	rows, err := db.sql.Query(q, threshold)
	if err != nil {
		return nil, fmt.Errorf("list expired sessions: %w", err)
	}
	defer rows.Close()
	return scanBranches(rows)
}

// ListExpiredLeases returns cli_reviewing branches whose durable lease_expires_at
// has passed. Used by the lease audit goroutine as a safety net when Redis keys
// are missing or keyspace notifications are unavailable.
func ListExpiredLeases(db *DB) ([]BranchRecord, error) {
	now := time.Now()
	const q = `
		SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
		       approved, ci_green, pending_events, unresolved_threads,
		       owner_session_id, owner_session_last_seen,
		       queue_priority, submission_time, lease_id, lease_expires_at,
		       created_at, updated_at
		FROM branch_states
		WHERE state = 'cli_reviewing'
		  AND lease_expires_at IS NOT NULL
		  AND lease_expires_at < ?
		ORDER BY lease_expires_at ASC`

	rows, err := db.sql.Query(q, now)
	if err != nil {
		return nil, fmt.Errorf("list expired leases: %w", err)
	}
	defer rows.Close()
	return scanBranches(rows)
}

// TransitionBranch applies a validated state transition and appends an audit record.
// The transition is rejected (ErrInvalidTransition) if not permitted by the canonical
// state machine. The from-state is verified against the current DB record.
func TransitionBranch(db *DB, id string, from, to State, trigger string) error {
	if err := ValidateTransition(from, to); err != nil {
		return err
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("transition branch: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify current state matches expected from-state.
	var currentState string
	if err := tx.QueryRow(`SELECT state FROM branch_states WHERE id = ?`, id).Scan(&currentState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBranchNotFound
		}
		return fmt.Errorf("transition branch: read state: %w", err)
	}
	if State(currentState) != from {
		return fmt.Errorf("%w: current state %q does not match expected from-state %q",
			ErrInvalidTransition, currentState, from)
	}

	// Apply the transition.
	_, err = tx.Exec(
		`UPDATE branch_states SET state = ?, updated_at = datetime('now') WHERE id = ?`,
		string(to), id,
	)
	if err != nil {
		return fmt.Errorf("transition branch: update state: %w", err)
	}

	// Append audit record.
	_, err = tx.Exec(
		`INSERT INTO state_transitions (branch_state_id, from_state, to_state, trigger)
		 VALUES (?, ?, ?, ?)`,
		id, string(from), string(to), trigger,
	)
	if err != nil {
		return fmt.Errorf("transition branch: insert audit: %w", err)
	}

	return tx.Commit()
}

// IncrementRetryCount increments retry_count for a branch and returns the new value.
// Uses RETURNING to read the new count atomically, avoiding an UPDATE+SELECT race.
func IncrementRetryCount(db *DB, id string) (int, error) {
	var count int
	if err := db.sql.QueryRow(
		`UPDATE branch_states SET retry_count = retry_count + 1, updated_at = datetime('now') WHERE id = ? RETURNING retry_count`,
		id,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("increment retry count: %w", err)
	}
	return count, nil
}

// ResetRetryCount resets retry_count to 0 (used on reactivate/re-submit).
func ResetRetryCount(db *DB, id string) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states SET retry_count = 0, updated_at = datetime('now') WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("reset retry count: %w", err)
	}
	return nil
}

// UpdateQueuePriority updates queue_priority for a branch record.
func UpdateQueuePriority(db *DB, id string, priority int) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states SET queue_priority = ?, updated_at = datetime('now') WHERE id = ?`,
		priority, id,
	)
	if err != nil {
		return fmt.Errorf("update queue priority: %w", err)
	}
	return nil
}

// UpdateLeaseInfo sets lease_id and lease_expires_at on a branch record.
// Called when a runner acquires a lease.
func UpdateLeaseInfo(db *DB, id, leaseID string, expiresAt time.Time) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states SET lease_id = ?, lease_expires_at = ?, updated_at = datetime('now') WHERE id = ?`,
		leaseID, expiresAt, id,
	)
	if err != nil {
		return fmt.Errorf("update lease info: %w", err)
	}
	return nil
}

// ClearLeaseInfo clears lease_id and lease_expires_at (lease released/expired).
func ClearLeaseInfo(db *DB, id string) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states SET lease_id = NULL, lease_expires_at = NULL, updated_at = datetime('now') WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("clear lease info: %w", err)
	}
	return nil
}

// UpdatePRNumber stores the GitHub PR number for the given branch.
func UpdatePRNumber(ctx context.Context, db *DB, repo, branch string, prNumber int) error {
	_, err := db.sql.ExecContext(ctx,
		`UPDATE branch_states SET pr_number = ?, updated_at = datetime('now') WHERE repo = ? AND branch = ?`,
		prNumber, repo, branch,
	)
	if err != nil {
		return fmt.Errorf("update pr number: %w", err)
	}
	return nil
}

// UpdateOwnerAgent records the agent identifier for the current owner session.
// A blank agentID is a no-op.
func UpdateOwnerAgent(ctx context.Context, db *DB, repo, branch, agentID string) error {
	if agentID == "" {
		return nil
	}
	_, err := db.sql.ExecContext(ctx,
		`UPDATE branch_states SET owner_agent = ?, updated_at = datetime('now') WHERE repo = ? AND branch = ?`,
		agentID, repo, branch,
	)
	if err != nil {
		return fmt.Errorf("update owner agent: %w", err)
	}
	return nil
}

// UpdateSessionHeartbeat records the current time as the last-seen timestamp for
// the branch owner session. Used by the CLI heartbeat command.
func UpdateSessionHeartbeat(db *DB, repo, branch string) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states SET owner_session_last_seen = datetime('now'), updated_at = datetime('now')
		 WHERE repo = ? AND branch = ?`,
		repo, branch,
	)
	if err != nil {
		return fmt.Errorf("update session heartbeat: %w", err)
	}
	return nil
}

// ClearBranchOwnership clears the branch-level session ownership markers.
func ClearBranchOwnership(ctx context.Context, db *DB, repo, branch string) error {
	_, err := db.sql.ExecContext(ctx, `
		UPDATE branch_states
		SET owner_session_id = '',
		    owner_session_last_seen = NULL,
		    owner_agent = '',
		    updated_at = datetime('now')
		WHERE repo = ? AND branch = ?`,
		repo, branch,
	)
	if err != nil {
		return fmt.Errorf("clear branch ownership: %w", err)
	}
	return nil
}

// UpdateMergeReadiness updates the four merge-readiness fields atomically.
func UpdateMergeReadiness(db *DB, id string, approved, ciGreen bool, pendingEvents, unresolvedThreads int) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states
		 SET approved = ?, ci_green = ?, pending_events = ?, unresolved_threads = ?,
		     updated_at = datetime('now')
		 WHERE id = ?`,
		boolInt(approved), boolInt(ciGreen), pendingEvents, unresolvedThreads, id,
	)
	if err != nil {
		return fmt.Errorf("update merge readiness: %w", err)
	}
	return nil
}

// UpdateHeadHashAndTransition atomically updates head_hash and applies a
// validated state transition with an audit record. Used by the reconciler for
// stale-branch detection to avoid a non-atomic UpdateHeadHash + TransitionBranch.
func UpdateHeadHashAndTransition(db *DB, id, newHeadHash string, from, to State, trigger string) error {
	if err := ValidateTransition(from, to); err != nil {
		return err
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("update head hash and transition: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentState string
	if err := tx.QueryRow(`SELECT state FROM branch_states WHERE id = ?`, id).Scan(&currentState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBranchNotFound
		}
		return fmt.Errorf("update head hash and transition: read state: %w", err)
	}
	if State(currentState) != from {
		return fmt.Errorf("%w: current state %q does not match expected from-state %q",
			ErrInvalidTransition, currentState, from)
	}

	_, err = tx.Exec(
		`UPDATE branch_states SET head_hash = ?, state = ?, updated_at = datetime('now') WHERE id = ?`,
		newHeadHash, string(to), id,
	)
	if err != nil {
		return fmt.Errorf("update head hash and transition: update: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO state_transitions (branch_state_id, from_state, to_state, trigger) VALUES (?, ?, ?, ?)`,
		id, string(from), string(to), trigger,
	)
	if err != nil {
		return fmt.Errorf("update head hash and transition: insert audit: %w", err)
	}

	return tx.Commit()
}

// UpdateHeadHash updates the head_hash field (stale detection).
func UpdateHeadHash(db *DB, id, headHash string) error {
	_, err := db.sql.Exec(
		`UPDATE branch_states SET head_hash = ?, updated_at = datetime('now') WHERE id = ?`,
		headHash, id,
	)
	if err != nil {
		return fmt.Errorf("update head hash: %w", err)
	}
	return nil
}

// --- Delivery events ---

// AppendDeliveryEvent inserts a delivery event with the given seq number.
func AppendDeliveryEvent(db *DB, ev DeliveryEvent) error {
	_, err := db.sql.Exec(
		`INSERT INTO delivery_events (seq, repo, branch, head_hash, event_type, payload)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ev.Seq, ev.Repo, ev.Branch, ev.HeadHash, ev.EventType, ev.Payload,
	)
	if err != nil {
		return fmt.Errorf("append delivery event: %w", err)
	}
	return nil
}

// GetDeliverySeqFloor returns the maximum seq stored durably for a repo+branch.
// Returns 0 if no events exist. Used on startup to ensure the Redis seq counter
// continues upward from the durable floor.
func GetDeliverySeqFloor(db *DB, repo, branch string) (int64, error) {
	var floor sql.NullInt64
	err := db.sql.QueryRow(
		`SELECT MAX(seq) FROM delivery_events WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&floor)
	if err != nil {
		return 0, fmt.Errorf("get delivery seq floor: %w", err)
	}
	if !floor.Valid {
		return 0, nil
	}
	return floor.Int64, nil
}

// ListDeliveryEvents returns delivery events for a repo+branch with seq > sinceSeq,
// ordered by seq ascending. Idempotent on repeated calls (append-only source).
func ListDeliveryEvents(db *DB, repo, branch string, sinceSeq int64) ([]DeliveryEvent, error) {
	const q = `
		SELECT seq, repo, branch, head_hash, event_type, payload, created_at
		FROM delivery_events
		WHERE repo = ? AND branch = ? AND seq > ?
		ORDER BY seq ASC`

	rows, err := db.sql.Query(q, repo, branch, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("list delivery events: %w", err)
	}
	defer rows.Close()

	var events []DeliveryEvent
	for rows.Next() {
		var ev DeliveryEvent
		if err := rows.Scan(
			&ev.Seq, &ev.Repo, &ev.Branch, &ev.HeadHash,
			&ev.EventType, &ev.Payload, &ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list delivery events: %w", err)
	}
	return events, nil
}

// --- Webhook dedup ---

// MarkWebhookDelivery records a webhook delivery in the durable dedup table.
// Returns false if the delivery_id was already recorded (already processed).
func MarkWebhookDelivery(db *DB, deliveryID, eventType, repo string) (bool, error) {
	res, err := db.sql.Exec(
		`INSERT OR IGNORE INTO webhook_deliveries (delivery_id, event_type, repo, processed)
		 VALUES (?, ?, ?, 1)`,
		deliveryID, eventType, repo,
	)
	if err != nil {
		return false, fmt.Errorf("mark webhook delivery: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil // true = newly inserted (not a duplicate)
}

// IsWebhookProcessed returns true if a delivery has already been processed.
func IsWebhookProcessed(ctx context.Context, db *DB, deliveryID string) (bool, error) {
	var count int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_deliveries WHERE delivery_id = ? AND processed = 1`,
		deliveryID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check webhook processed: %w", err)
	}
	return count > 0, nil
}

// --- Review runs ---

// CreateReviewRun inserts a new review run record.
func CreateReviewRun(db *DB, run *ReviewRun) error {
	_, err := db.sql.Exec(
		`INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.Repo, run.Branch, run.HeadHash, run.Provider, run.Status, run.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("create review run: %w", err)
	}
	return nil
}

// UpdateReviewRun updates the terminal status of a review run.
func UpdateReviewRun(db *DB, id, status, errMsg string, finishedAt time.Time) error {
	_, err := db.sql.Exec(
		`UPDATE review_runs SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		status, errMsg, finishedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update review run: %w", err)
	}
	return nil
}

// --- Findings ---

// InsertFinding inserts a normalized finding record.
func InsertFinding(db *DB, f *FindingRecord) error {
	_, err := db.sql.Exec(
		`INSERT INTO findings (id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.RunID, f.Repo, f.Branch, f.Severity, f.Category,
		f.File, f.Line, f.Message, f.Source, f.RuleID, f.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert finding: %w", err)
	}
	return nil
}

// ListFindings returns all findings for a repo+branch ordered by timestamp.
func ListFindings(db *DB, repo, branch string) ([]FindingRecord, error) {
	const q = `
		SELECT id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts, created_at
		FROM findings
		WHERE repo = ? AND branch = ?
		ORDER BY ts ASC`

	rows, err := db.sql.Query(q, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()

	var findings []FindingRecord
	for rows.Next() {
		var f FindingRecord
		if err := rows.Scan(
			&f.ID, &f.RunID, &f.Repo, &f.Branch, &f.Severity, &f.Category,
			&f.File, &f.Line, &f.Message, &f.Source, &f.RuleID, &f.Timestamp, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		findings = append(findings, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	return findings, nil
}

// InsertFindings inserts multiple normalized finding records in a single transaction.
func InsertFindings(db *DB, findings []*FindingRecord) error {
	if len(findings) == 0 {
		return nil
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("insert findings: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(
		`INSERT INTO findings (id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("insert findings: prepare: %w", err)
	}
	defer stmt.Close()

	for _, f := range findings {
		if _, err := stmt.Exec(
			f.ID, f.RunID, f.Repo, f.Branch, f.Severity, f.Category,
			f.File, f.Line, f.Message, f.Source, f.RuleID, f.Timestamp,
		); err != nil {
			return fmt.Errorf("insert findings: exec: %w", err)
		}
	}
	return tx.Commit()
}

// --- helpers ---

func scanBranch(row *sql.Row) (*BranchRecord, error) {
	var b BranchRecord
	var approvedInt, ciGreenInt int
	var ownerSessionLastSeen, submissionTime, leaseExpiresAt sql.NullTime
	var leaseID sql.NullString

	err := row.Scan(
		&b.ID, &b.Repo, &b.Branch, &b.HeadHash, &b.State,
		&b.RetryCount, &b.MaxRetries,
		&approvedInt, &ciGreenInt, &b.PendingEvents, &b.UnresolvedThreads,
		&b.OwnerSessionID, &ownerSessionLastSeen,
		&b.QueuePriority, &submissionTime, &leaseID, &leaseExpiresAt,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBranchNotFound
		}
		return nil, fmt.Errorf("scan branch: %w", err)
	}

	b.Approved = approvedInt != 0
	b.CIGreen = ciGreenInt != 0
	if ownerSessionLastSeen.Valid {
		b.OwnerSessionLastSeen = &ownerSessionLastSeen.Time
	}
	if submissionTime.Valid {
		b.SubmissionTime = &submissionTime.Time
	}
	if leaseID.Valid {
		b.LeaseID = &leaseID.String
	}
	if leaseExpiresAt.Valid {
		b.LeaseExpiresAt = &leaseExpiresAt.Time
	}
	return &b, nil
}

func scanBranches(rows *sql.Rows) ([]BranchRecord, error) {
	var records []BranchRecord
	for rows.Next() {
		var b BranchRecord
		var approvedInt, ciGreenInt int
		var ownerSessionLastSeen, submissionTime, leaseExpiresAt sql.NullTime
		var leaseID sql.NullString

		err := rows.Scan(
			&b.ID, &b.Repo, &b.Branch, &b.HeadHash, &b.State,
			&b.RetryCount, &b.MaxRetries,
			&approvedInt, &ciGreenInt, &b.PendingEvents, &b.UnresolvedThreads,
			&b.OwnerSessionID, &ownerSessionLastSeen,
			&b.QueuePriority, &submissionTime, &leaseID, &leaseExpiresAt,
			&b.CreatedAt, &b.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan branch row: %w", err)
		}
		b.Approved = approvedInt != 0
		b.CIGreen = ciGreenInt != 0
		if ownerSessionLastSeen.Valid {
			b.OwnerSessionLastSeen = &ownerSessionLastSeen.Time
		}
		if submissionTime.Valid {
			b.SubmissionTime = &submissionTime.Time
		}
		if leaseID.Valid {
			b.LeaseID = &leaseID.String
		}
		if leaseExpiresAt.Valid {
			b.LeaseExpiresAt = &leaseExpiresAt.Time
		}
		records = append(records, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan branches: %w", err)
	}
	return records, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
