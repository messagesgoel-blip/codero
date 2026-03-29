package sessmetrics

import (
	"context"
	"fmt"
	"strings"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// modelContextLimit returns the known context window token limit for a model.
// Returns 0 if the model is unknown (pressure check is skipped).
func modelContextLimit(model string) int64 {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude-opus-4") || strings.Contains(m, "claude-sonnet-4") || strings.Contains(m, "claude-haiku-4"):
		return 200_000
	case strings.Contains(m, "claude-3-5") || strings.Contains(m, "claude-3.5"):
		return 200_000
	case strings.Contains(m, "claude-3"):
		return 200_000
	case strings.Contains(m, "gpt-4o") || strings.Contains(m, "gpt-4-turbo"):
		return 128_000
	case strings.Contains(m, "gpt-4"):
		return 8_192
	case strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") ||
		strings.Contains(m, "/o1") || strings.Contains(m, "/o3"):
		return 200_000
	case strings.Contains(m, "gemini-2.5"):
		return 1_000_000
	case strings.Contains(m, "gemini-2.0") || strings.Contains(m, "gemini-1.5"):
		return 1_000_000
	case strings.Contains(m, "gemini"):
		return 32_768
	case strings.Contains(m, "qwen3") || strings.Contains(m, "qwen2.5"):
		return 128_000
	case strings.Contains(m, "kimi") || strings.Contains(m, "moonshot"):
		return 128_000
	case strings.Contains(m, "glm"):
		return 128_000
	case strings.Contains(m, "deepseek"):
		return 64_000
	case strings.Contains(m, "llama-3.3") || strings.Contains(m, "llama-3.1"):
		return 128_000
	default:
		return 0
	}
}

// PressureConfig controls the thresholds for context pressure detection.
type PressureConfig struct {
	// WarningPct triggers "warning" when cumulative prompt tokens exceed this
	// fraction of the model context limit. Default: 0.65.
	WarningPct float64
	// CriticalPct triggers "critical". Default: 0.85.
	CriticalPct float64
	// TrendWindow is the number of most-recent requests used for inflation
	// trend detection. Default: 5.
	TrendWindow int
	// TrendInflationFactor: if the mean prompt_tokens of the last TrendWindow
	// requests is >= TrendInflationFactor * session mean, emit a warning even
	// if the threshold hasn't been reached. Default: 1.5.
	TrendInflationFactor float64
}

func defaultPressureConfig() PressureConfig {
	return PressureConfig{
		WarningPct:           0.65,
		CriticalPct:          0.85,
		TrendWindow:          5,
		TrendInflationFactor: 1.5,
	}
}

// PressureDetector evaluates context pressure for active sessions.
type PressureDetector struct {
	db  *state.DB
	cfg PressureConfig
}

// NewPressureDetector returns a detector with default thresholds.
func NewPressureDetector(db *state.DB) *PressureDetector {
	return &PressureDetector{db: db, cfg: defaultPressureConfig()}
}

// WithConfig overrides the default thresholds.
func (d *PressureDetector) WithConfig(cfg PressureConfig) *PressureDetector {
	d.cfg = cfg
	return d
}

// EvaluateAll checks all active sessions and updates context_pressure.
func (d *PressureDetector) EvaluateAll(ctx context.Context) error {
	sessions, err := state.ListActiveAgentSessions(ctx, d.db)
	if err != nil {
		return fmt.Errorf("list active agent sessions: %w", err)
	}
	for _, sess := range sessions {
		if err := d.evaluate(ctx, sess.SessionID); err != nil {
			loglib.Warn("sessmetrics: pressure eval failed",
				loglib.FieldComponent, "sessmetrics",
				"session_id", sess.SessionID,
				"error", err,
			)
		}
	}
	return nil
}

func (d *PressureDetector) evaluate(ctx context.Context, sessionID string) error {
	rows, err := state.GetTokenMetrics(ctx, d.db, sessionID)
	if err != nil {
		return fmt.Errorf("get token metrics for session %s: %w", sessionID, err)
	}
	if len(rows) == 0 {
		return nil
	}

	last := rows[len(rows)-1]
	level := state.ContextPressureNormal

	// ── Threshold check ──────────────────────────────────────────────────────
	// Use the most common model from recent requests to get the context limit.
	limit := modelContextLimit(last.Model)
	if limit > 0 {
		pct := float64(last.CumulativePromptTokens) / float64(limit)
		switch {
		case pct >= d.cfg.CriticalPct:
			level = state.ContextPressureCritical
		case pct >= d.cfg.WarningPct:
			level = state.ContextPressureWarning
		}
	}

	// ── Trend check (token inflation) ────────────────────────────────────────
	if level == state.ContextPressureNormal && len(rows) > d.cfg.TrendWindow {
		var sessionSum float64
		for _, r := range rows {
			sessionSum += float64(r.PromptTokens)
		}
		sessionMean := sessionSum / float64(len(rows))

		recent := rows[len(rows)-d.cfg.TrendWindow:]
		var recentSum float64
		for _, r := range recent {
			recentSum += float64(r.PromptTokens)
		}
		recentMean := recentSum / float64(len(recent))

		if sessionMean > 0 && recentMean >= d.cfg.TrendInflationFactor*sessionMean {
			level = state.ContextPressureWarning
		}
	}

	return state.SetContextPressure(ctx, d.db, sessionID, level)
}
