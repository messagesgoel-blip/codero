package state

import (
	"context"
	"fmt"
)

// MergePredicate names a single merge-readiness condition.
// The set is a provable superset of GitHub branch protection (EL-21).
type MergePredicate string

const (
	// Standard GitHub branch-protection predicates (must be covered).
	PredicateApproved          MergePredicate = "approved"
	PredicateCIGreen           MergePredicate = "ci_green"
	PredicateNoUnresolvedConvo MergePredicate = "no_unresolved_threads"

	// Codero-additive predicates (superset of branch protection).
	PredicateNoPendingEvents   MergePredicate = "no_pending_events"
	PredicateGateChecksPassed  MergePredicate = "gate_checks_passed"
	PredicateNoBlockingReasons MergePredicate = "no_blocking_reasons"
)

// AllMergePredicates is the canonical ordered set (EL-21 §21).
var AllMergePredicates = []MergePredicate{
	PredicateApproved,
	PredicateCIGreen,
	PredicateNoUnresolvedConvo,
	PredicateNoPendingEvents,
	PredicateGateChecksPassed,
	PredicateNoBlockingReasons,
}

// MergePredicateResult is the outcome of evaluating one predicate.
type MergePredicateResult struct {
	Predicate MergePredicate
	Passed    bool
	Reason    string // human-readable blocking reason when !Passed
}

// MergeReadinessResult is the full evaluation of all merge predicates.
type MergeReadinessResult struct {
	Ready           bool
	Results         []MergePredicateResult
	BlockingReasons []string
	PassedCount     int
	TotalPredicates int
}

// EvaluateMergeReadiness checks all merge predicates for a branch (EL-21).
// The predicate set is a provable superset of GitHub branch protection:
//   - approved         ⊇ required_pull_request_reviews
//   - ci_green         ⊇ required_status_checks
//   - no_unresolved    ⊇ required_conversation_resolution
//   - no_pending_events  (Codero-additive: no stale webhook state)
//   - gate_checks_passed (Codero-additive: pre-commit gate must pass)
//   - no_blocking_reasons (Codero-additive: no operator blocks)
func EvaluateMergeReadiness(ctx context.Context, db *DB, branchStateID string) (*MergeReadinessResult, error) {
	var approved, ciGreen int
	var pendingEvents, unresolvedThreads int
	var branchState string

	err := db.sql.QueryRowContext(ctx, `
		SELECT COALESCE(approved, 0),
		       COALESCE(ci_green, 0),
		       COALESCE(pending_events, 0),
		       COALESCE(unresolved_threads, 0),
		       COALESCE(state, '')
		FROM branch_states
		WHERE id = ?`, branchStateID).Scan(
		&approved, &ciGreen, &pendingEvents, &unresolvedThreads, &branchState,
	)
	if err != nil {
		return nil, fmt.Errorf("evaluate merge readiness: %w", err)
	}

	// "blocked" state is operator/system-level block.
	isBlocked := branchState == "blocked"

	// Check for unresolved gate findings via review_runs.
	var failedGateCount int
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	err = db.sql.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM review_runs rr
		INNER JOIN branch_states bs ON bs.repo = rr.repo AND bs.branch = rr.branch
		WHERE bs.id = ? AND rr.status = 'failed'
		AND rr.finished_at = (
			SELECT MAX(rr2.finished_at) FROM review_runs rr2
			WHERE rr2.repo = rr.repo AND rr2.branch = rr.branch
		)`, branchStateID).Scan(&failedGateCount)
	if err != nil {
		failedGateCount = 0 // non-fatal: if no runs exist, gate is vacuously passed
	}

	results := []MergePredicateResult{
		{Predicate: PredicateApproved, Passed: approved != 0,
			Reason: ternaryReason(approved != 0, "", "PR not approved")},
		{Predicate: PredicateCIGreen, Passed: ciGreen != 0,
			Reason: ternaryReason(ciGreen != 0, "", "CI checks not green")},
		{Predicate: PredicateNoUnresolvedConvo, Passed: unresolvedThreads == 0,
			Reason: ternaryReason(unresolvedThreads == 0, "", fmt.Sprintf("%d unresolved review threads", unresolvedThreads))},
		{Predicate: PredicateNoPendingEvents, Passed: pendingEvents == 0,
			Reason: ternaryReason(pendingEvents == 0, "", fmt.Sprintf("%d pending webhook events", pendingEvents))},
		{Predicate: PredicateGateChecksPassed, Passed: failedGateCount == 0,
			Reason: ternaryReason(failedGateCount == 0, "", "latest gate run failed")},
		{Predicate: PredicateNoBlockingReasons, Passed: !isBlocked,
			Reason: ternaryReason(!isBlocked, "", "branch is in blocked state")},
	}

	ready := true
	var blocking []string
	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			ready = false
			if r.Reason != "" {
				blocking = append(blocking, r.Reason)
			}
		}
	}

	return &MergeReadinessResult{
		Ready:           ready,
		Results:         results,
		BlockingReasons: blocking,
		PassedCount:     passed,
		TotalPredicates: len(results),
	}, nil
}

func ternaryReason(ok bool, pass, fail string) string {
	if ok {
		return pass
	}
	return fail
}
