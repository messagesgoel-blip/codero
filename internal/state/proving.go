package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type PrecommitReview struct {
	ID        string
	Repo      string
	Branch    string
	Provider  string
	Status    string
	Error     string
	CreatedAt time.Time
}

type ProvingEvent struct {
	ID        int64
	Repo      string
	EventType string
	Details   string
	CreatedAt time.Time
}

type ProvingSnapshot struct {
	ID            int64
	SnapshotDate  string
	ScorecardJSON string
	CreatedAt     time.Time
}

type ProvingScorecard struct {
	GeneratedAt              time.Time      `json:"generated_at"`
	PeriodStart              time.Time      `json:"period_start"`
	PeriodEnd                time.Time      `json:"period_end"`
	BranchesReviewed7Days    int            `json:"branches_reviewed_7_days"`
	BranchesReviewedByRepo   map[string]int `json:"branches_reviewed_by_repo"`
	StaleDetections30Days    int            `json:"stale_detections_30_days"`
	LeaseExpiryRecoveries    int            `json:"lease_expiry_recoveries_30_days"`
	PrecommitReviewsByRepo   map[string]int `json:"precommit_reviews_by_repo"`
	PrecommitReviews7Days    int            `json:"precommit_reviews_7_days"`
	MissedFeedbackDeliveries int            `json:"missed_feedback_deliveries"`
	QueueStallIncidents      int            `json:"queue_stall_incidents_30_days"`
	UnresolvedThreadFailures int            `json:"unresolved_thread_failures_30_days"`
	ManualDBRepairs          int            `json:"manual_db_repairs_30_days"`
}

func CreatePrecommitReview(ctx context.Context, db *DB, r *PrecommitReview) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO precommit_reviews (id, repo, branch, provider, status, error)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.Repo, r.Branch, r.Provider, r.Status, r.Error,
	)
	if err != nil {
		return fmt.Errorf("create precommit review: %w", err)
	}
	return nil
}

// CreatePrecommitReviewIdempotent inserts a precommit review record, silently
// skipping the insert if a record with the same ID already exists.
// This is used by the commit-gate auto-write path to prevent duplicate entries
// when the same run_id is observed more than once (e.g. during polling retries).
func CreatePrecommitReviewIdempotent(ctx context.Context, db *DB, r *PrecommitReview) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO precommit_reviews (id, repo, branch, provider, status, error)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.Repo, r.Branch, r.Provider, r.Status, r.Error,
	)
	if err != nil {
		return fmt.Errorf("create precommit review (idempotent): %w", err)
	}
	return nil
}

func ListPrecommitReviewsByRepo(ctx context.Context, db *DB, repo string, since time.Time) ([]PrecommitReview, error) {
	const q = `
		SELECT id, repo, branch, provider, status, error, created_at
		FROM precommit_reviews
		WHERE repo = ? AND created_at >= ?
		ORDER BY created_at DESC`

	rows, err := db.sql.QueryContext(ctx, q, repo, since)
	if err != nil {
		return nil, fmt.Errorf("list precommit reviews: %w", err)
	}
	defer rows.Close()

	var reviews []PrecommitReview
	for rows.Next() {
		var r PrecommitReview
		if err := rows.Scan(&r.ID, &r.Repo, &r.Branch, &r.Provider, &r.Status, &r.Error, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan precommit review: %w", err)
		}
		reviews = append(reviews, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list precommit reviews: %w", err)
	}
	return reviews, nil
}

func CountPrecommitReviews(ctx context.Context, db *DB, since time.Time) (int, map[string]int, error) {
	const q = `
		SELECT COUNT(*) as total, repo as repo
		FROM precommit_reviews
		WHERE created_at >= ?
		GROUP BY repo`

	rows, err := db.sql.QueryContext(ctx, q, since)
	if err != nil {
		return 0, nil, fmt.Errorf("count precommit reviews: %w", err)
	}
	defer rows.Close()

	total := 0
	byRepo := make(map[string]int)
	for rows.Next() {
		var count int
		var repo string
		if err := rows.Scan(&count, &repo); err != nil {
			return 0, nil, fmt.Errorf("scan precommit count: %w", err)
		}
		total += count
		byRepo[repo] = count
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("count precommit reviews: %w", err)
	}
	return total, byRepo, nil
}

func CreateProvingEvent(ctx context.Context, db *DB, eventType, repo, details string) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO proving_events (repo, event_type, details) VALUES (?, ?, ?)`,
		repo, eventType, details,
	)
	if err != nil {
		return fmt.Errorf("create proving event: %w", err)
	}
	return nil
}

func CountProvingEvents(ctx context.Context, db *DB, eventType string, since time.Time) (int, error) {
	var count int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM proving_events WHERE event_type = ? AND created_at >= ?`,
		eventType, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count proving events: %w", err)
	}
	return count, nil
}

func CountProvingEventsByType(ctx context.Context, db *DB, eventTypes []string, since time.Time) (map[string]int, error) {
	if len(eventTypes) == 0 {
		return make(map[string]int), nil
	}

	result := make(map[string]int)
	for _, et := range eventTypes {
		result[et] = 0
	}

	placeholders := make([]string, len(eventTypes))
	args := make([]interface{}, 0, len(eventTypes)+1)
	args = append(args, since)
	for i, et := range eventTypes {
		placeholders[i] = "?"
		args = append(args, et)
	}

	query := fmt.Sprintf(
		"SELECT event_type, COUNT(*) FROM proving_events WHERE created_at >= ? AND event_type IN (%s) GROUP BY event_type",
		joinStrings(placeholders, ","),
	)

	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("count proving events by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var eventType string
		var count int
		if err := rows.Scan(&eventType, &count); err != nil {
			return nil, fmt.Errorf("scan proving event count: %w", err)
		}
		result[eventType] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("count proving events by type: %w", err)
	}

	return result, nil
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

func SaveProvingSnapshot(ctx context.Context, db *DB, date, scorecardJSON string) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO proving_snapshots (snapshot_date, scorecard_json) VALUES (?, ?)`,
		date, scorecardJSON,
	)
	if err != nil {
		return fmt.Errorf("save proving snapshot: %w", err)
	}
	return nil
}

func GetProvingSnapshot(ctx context.Context, db *DB, date string) (*ProvingSnapshot, error) {
	const q = `
		SELECT id, snapshot_date, scorecard_json, created_at
		FROM proving_snapshots
		WHERE snapshot_date = ?`

	row := db.sql.QueryRowContext(ctx, q, date)
	var s ProvingSnapshot
	err := row.Scan(&s.ID, &s.SnapshotDate, &s.ScorecardJSON, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get proving snapshot: %w", err)
	}
	return &s, nil
}

func ListProvingSnapshots(ctx context.Context, db *DB, since time.Time) ([]ProvingSnapshot, error) {
	const q = `
		SELECT id, snapshot_date, scorecard_json, created_at
		FROM proving_snapshots
		WHERE created_at >= ?
		ORDER BY snapshot_date ASC`

	rows, err := db.sql.QueryContext(ctx, q, since)
	if err != nil {
		return nil, fmt.Errorf("list proving snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []ProvingSnapshot
	for rows.Next() {
		var s ProvingSnapshot
		if err := rows.Scan(&s.ID, &s.SnapshotDate, &s.ScorecardJSON, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proving snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list proving snapshots: %w", err)
	}
	return snapshots, nil
}

func CountBranchesReviewed(ctx context.Context, db *DB, since time.Time) (int, map[string]int, error) {
	const q = `
		SELECT COUNT(DISTINCT repo || '/' || branch) as total, repo as repo
		FROM review_runs
		WHERE started_at >= ? AND status = 'completed'
		GROUP BY repo`

	rows, err := db.sql.QueryContext(ctx, q, since)
	if err != nil {
		return 0, nil, fmt.Errorf("count branches reviewed: %w", err)
	}
	defer rows.Close()

	total := 0
	byRepo := make(map[string]int)
	for rows.Next() {
		var count int
		var repo string
		if err := rows.Scan(&count, &repo); err != nil {
			return 0, nil, fmt.Errorf("scan reviewed count: %w", err)
		}
		total += count
		byRepo[repo] = count
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("count branches reviewed: %w", err)
	}
	return total, byRepo, nil
}

func CountStaleDetections(ctx context.Context, db *DB, since time.Time) (int, error) {
	var count int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM state_transitions WHERE to_state = 'stale_branch' AND created_at >= ?`,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count stale detections: %w", err)
	}
	return count, nil
}

func CountLeaseExpiryRecoveries(ctx context.Context, db *DB, since time.Time) (int, error) {
	var count int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM state_transitions 
		 WHERE trigger = 'lease_expired' AND created_at >= ?`,
		since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count lease expiry recoveries: %w", err)
	}
	return count, nil
}

func CountMissedDeliveries(ctx context.Context, db *DB, since time.Time) (int, error) {
	const q = `
		SELECT COUNT(*) FROM proving_events 
		WHERE event_type = 'missed_delivery' AND created_at >= ?`

	var count int
	err := db.sql.QueryRowContext(ctx, q, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count missed deliveries: %w", err)
	}
	return count, nil
}

func ComputeProvingScorecard(ctx context.Context, db *DB) (*ProvingScorecard, error) {
	now := time.Now()
	sevenDays := now.AddDate(0, 0, -7)
	thirtyDays := now.AddDate(0, 0, -30)

	card := &ProvingScorecard{
		GeneratedAt:            now,
		PeriodStart:            thirtyDays,
		PeriodEnd:              now,
		BranchesReviewedByRepo: make(map[string]int),
		PrecommitReviewsByRepo: make(map[string]int),
	}

	branchesReviewed, byRepo, err := CountBranchesReviewed(ctx, db, sevenDays)
	if err != nil {
		return nil, fmt.Errorf("scorecard: branches reviewed: %w", err)
	}
	card.BranchesReviewed7Days = branchesReviewed
	card.BranchesReviewedByRepo = byRepo

	staleCount, err := CountStaleDetections(ctx, db, thirtyDays)
	if err != nil {
		return nil, fmt.Errorf("scorecard: stale detections: %w", err)
	}
	card.StaleDetections30Days = staleCount

	leaseRecoveries, err := CountLeaseExpiryRecoveries(ctx, db, thirtyDays)
	if err != nil {
		return nil, fmt.Errorf("scorecard: lease expiry recoveries: %w", err)
	}
	card.LeaseExpiryRecoveries = leaseRecoveries

	precommitTotal, precommitByRepo, err := CountPrecommitReviews(ctx, db, sevenDays)
	if err != nil {
		return nil, fmt.Errorf("scorecard: precommit reviews: %w", err)
	}
	card.PrecommitReviews7Days = precommitTotal
	card.PrecommitReviewsByRepo = precommitByRepo

	missedCount, err := CountMissedDeliveries(ctx, db, thirtyDays)
	if err != nil {
		return nil, fmt.Errorf("scorecard: missed deliveries: %w", err)
	}
	card.MissedFeedbackDeliveries = missedCount

	eventCounts, err := CountProvingEventsByType(ctx, db, []string{
		"queue_stall",
		"unresolved_thread_failure",
		"manual_db_repair",
	}, thirtyDays)
	if err != nil {
		return nil, fmt.Errorf("scorecard: proving events: %w", err)
	}
	card.QueueStallIncidents = eventCounts["queue_stall"]
	card.UnresolvedThreadFailures = eventCounts["unresolved_thread_failure"]
	card.ManualDBRepairs = eventCounts["manual_db_repair"]

	return card, nil
}

// CountConsecutiveDays returns the number of consecutive calendar days (ending today)
// for which a proving snapshot exists. Used to track the 30-day streak requirement.
func CountConsecutiveDays(ctx context.Context, db *DB) (int, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT snapshot_date FROM proving_snapshots ORDER BY snapshot_date DESC`)
	if err != nil {
		return 0, fmt.Errorf("count consecutive days: %w", err)
	}
	defer rows.Close()

	streak := 0
	expected := time.Now().Format("2006-01-02")
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return 0, fmt.Errorf("count consecutive days: scan: %w", err)
		}
		if date != expected {
			break
		}
		streak++
		t, _ := time.Parse("2006-01-02", date)
		expected = t.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return streak, rows.Err()
}

// CountActiveRepos returns the count of distinct repos that have at least one
// precommit review record within the given time window. This measures whether
// all managed repositories are actively being proved.
func CountActiveRepos(ctx context.Context, db *DB, since time.Time) (int, error) {
	var count int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT repo) FROM precommit_reviews WHERE created_at >= ?`,
		since.UTC().Format("2006-01-02 15:04:05"),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active repos: %w", err)
	}
	return count, nil
}

// SnapshotExistsForDate returns true when a proving snapshot for the given YYYY-MM-DD
// date already exists in the database. Used by daily-snapshot for idempotency.
func SnapshotExistsForDate(ctx context.Context, db *DB, date string) (bool, error) {
	var count int
	err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM proving_snapshots WHERE snapshot_date = ?`, date,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("snapshot exists check: %w", err)
	}
	return count > 0, nil
}
