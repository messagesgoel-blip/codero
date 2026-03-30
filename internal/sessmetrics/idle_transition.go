package sessmetrics

import (
	"context"
	"time"

	"github.com/codero/codero/internal/state"
)

const idleThreshold = 90 * time.Second

// transitionIdleSessions marks active sessions as idle when both last_io_at
// and inferred_status_updated_at are older than the idle threshold.
//
// Write authority: the monitor is the ONLY writer of "idle" status.
// Guards (see plan Decision 3):
//   - Never overrides waiting_for_input (operator needs to handle it)
//   - Never targets unknown (session hasn't started yet)
//   - Requires last_io_at IS NOT NULL (sessions that never produced output excluded)
//   - Requires inferred_status_updated_at IS NOT NULL (new sessions excluded)
//   - Both timestamps must be older than threshold
func transitionIdleSessions(ctx context.Context, db *state.DB) error {
	now := time.Now().UTC()
	threshold := now.Add(-idleThreshold)
	return state.TransitionIdleSessions(ctx, db, now, threshold)
}
