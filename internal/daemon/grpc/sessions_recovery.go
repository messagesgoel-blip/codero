package grpc

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/codero/codero/internal/state"
)

// SessionRecoveryService handles session state restoration after daemon restart
type SessionRecoveryService struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSessionRecoveryService creates a new session recovery service
func NewSessionRecoveryService(db *sql.DB, logger *slog.Logger) *SessionRecoveryService {
	return &SessionRecoveryService{
		db:     db,
		logger: logger,
	}
}

// RecoverActiveSessions restores session continuity after daemon restart
// This method identifies sessions that were active before the restart and
// ensures they can be properly reconnected by agents
func (s *SessionRecoveryService) RecoverActiveSessions(ctx context.Context) error {
	if s.logger != nil {
		s.logger.Info("starting session recovery after daemon restart")
	}

	// Find all sessions that were active (not ended) before the restart
	activeSessions, err := s.getActiveSessions(ctx)
	if err != nil {
		return fmt.Errorf("recover active sessions: %w", err)
	}

	recoveredCount := 0
	for _, session := range activeSessions {
		if err := s.recoverSession(ctx, session); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to recover session",
					"session_id", session.SessionID,
					"agent_id", session.AgentID,
					"error", err)
			}
			continue
		}
		recoveredCount++
	}

	if s.logger != nil {
		s.logger.Info("session recovery completed",
			"total_active", len(activeSessions),
			"recovered", recoveredCount)
	}

	return nil
}

// getActiveSessions returns all sessions that were active (not ended) at the time of restart
func (s *SessionRecoveryService) getActiveSessions(ctx context.Context) ([]*state.AgentSession, error) {
	query := `
		SELECT session_id, agent_id, mode, started_at, last_seen_at, 
		       ended_at, end_reason, last_progress_at, tmux_session_name,
		       last_io_at, inferred_status, inferred_status_updated_at
		FROM agent_sessions 
		WHERE ended_at IS NULL
		ORDER BY last_seen_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*state.AgentSession
	for rows.Next() {
		var sess state.AgentSession
		var lastProgress sql.NullTime
		var endedAt sql.NullTime
		var endReason sql.NullString
		var tmuxSessionName sql.NullString
		var lastIO sql.NullTime
		var inferredStatus sql.NullString
		var inferredStatusUpdatedAt sql.NullTime

		err := rows.Scan(
			&sess.SessionID,
			&sess.AgentID,
			&sess.Mode,
			&sess.StartedAt,
			&sess.LastSeenAt,
			&endedAt,
			&endReason,
			&lastProgress,
			&tmuxSessionName,
			&lastIO,
			&inferredStatus,
			&inferredStatusUpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}

		// Convert null values to appropriate defaults
		if endedAt.Valid {
			sess.EndedAt = &endedAt.Time
		}
		if endReason.Valid {
			sess.EndReason = endReason.String
		}
		if lastProgress.Valid {
			sess.LastProgressAt = &lastProgress.Time
		}
		if tmuxSessionName.Valid {
			sess.TmuxSessionName = tmuxSessionName.String
		}
		if lastIO.Valid {
			sess.LastIOAt = &lastIO.Time
		}
		if inferredStatus.Valid {
			sess.InferredStatus = inferredStatus.String
		}
		if inferredStatusUpdatedAt.Valid {
			sess.InferredStatusUpdatedAt = &inferredStatusUpdatedAt.Time
		}

		sessions = append(sessions, &sess)
	}

	return sessions, rows.Err()
}

// recoverSession performs recovery operations for a single session
func (s *SessionRecoveryService) recoverSession(ctx context.Context, session *state.AgentSession) error {
	// If the session has a tmux session name, verify it still exists
	if session.TmuxSessionName != "" {
		// For now, we just log that we found a tmux session - actual verification would
		// require calling tmux commands which is handled by the expiry checker
		if s.logger != nil {
			s.logger.Debug("found session with tmux name during recovery",
				"session_id", session.SessionID,
				"tmux_session_name", session.TmuxSessionName)
		}
	}

	// Update last seen time to current time to indicate the session is being recovered
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		UPDATE agent_sessions 
		SET last_seen_at = ?
		WHERE session_id = ? AND ended_at IS NULL
	`, now, session.SessionID); err != nil {
		return fmt.Errorf("update last seen time: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("session recovered successfully",
			"session_id", session.SessionID,
			"agent_id", session.AgentID,
			"mode", session.Mode,
			"tmux_session", session.TmuxSessionName)
	}

	return nil
}

// IsSessionRecoverable checks if a session can be recovered after restart
func (s *SessionRecoveryService) IsSessionRecoverable(ctx context.Context, sessionID, agentID string) (bool, error) {
	var isEnded bool

	err := s.db.QueryRowContext(ctx, `
		SELECT ended_at IS NOT NULL as is_ended
		FROM agent_sessions 
		WHERE session_id = ? AND agent_id = ?
	`, sessionID, agentID).Scan(&isEnded)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check session recoverable: %w", err)
	}

	// Session is recoverable if it exists and is not ended
	return !isEnded, nil
}
